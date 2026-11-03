"""Runs only INSIDE the disposable root Debian PAM test container.
Never run on a workstation: this test intentionally creates users and activates PAM.
"""
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import ssl
import stat
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

assert os.geteuid() == 0 and os.environ.get('LINAD_ISOLATED_PAM_TEST') == '1' and Path('/.dockerenv').exists()
ROOT = Path('/src')
AGENT = '/usr/local/libexec/linad-linux'
CONFIG = Path('/etc/linad-linux/client.json')

def run(args, input=None, ok=True):
    result = subprocess.run(args, input=input, text=True, capture_output=True, timeout=40)
    if (result.returncode == 0) != ok:
        raise AssertionError(f'{args[0]} unexpected exit {result.returncode}: {result.stdout} {result.stderr}')
    return result

with tempfile.TemporaryDirectory() as tmp:
    cert, key = tmp + '/cert.pem', tmp + '/key.pem'
    run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
         '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1', '-keyout', key, '-out', cert])
    base = 'https://127.0.0.1:18443'
    context = ssl.create_default_context(cafile=cert)
    admin_password, user_password, local_password = [secrets.token_urlsafe(24) for _ in range(3)]
    env = dict(os.environ, LINAD_ADMIN_PASSWORD=admin_password, LINAD_DATA=tmp+'/directory.json',
               LINAD_LISTEN='127.0.0.1:18443', LINAD_TLS_CERT=cert, LINAD_TLS_KEY=key)
    server = subprocess.Popen([str(ROOT/'dist/linad-server')], env=env,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    daemon = None
    try:
        for _ in range(50):
            try:
                urllib.request.urlopen(base+'/healthz', context=context, timeout=1)
                break
            except Exception:
                time.sleep(.1)
        else:
            raise AssertionError('server failed to start')
        token = ''
        def api(method, path, data=None, want=200):
            headers = {'Content-Type':'application/json'}
            if token:
                headers['Authorization'] = 'Bearer '+token
            request = urllib.request.Request(base+path, method=method, headers=headers,
                        data=None if data is None else json.dumps(data).encode())
            try:
                response = urllib.request.urlopen(request, context=context, timeout=15)
            except urllib.error.HTTPError as error:
                response = error
            payload = json.load(response)
            assert response.status == want, (method, path, response.status)
            return payload
        token = api('POST','/v1/login',{'name':'admin','password':admin_password})['token']
        for name in ['alice','bob']:
            api('POST','/v1/users',{'name':name,'password':user_password})
        users = {u['name']:u for u in api('GET','/v1/users')}
        device = api('POST','/v1/devices',{'name':'test-linux','os':'linux'},201)
        prefix = '/v1/devices/'+device['id']
        enrollment = api('POST',prefix+'/enrollment')
        # A wrong/untrusted CA must reject enrollment without consuming the token.
        run([str(ROOT/'dist/linad-linux-amd64'),'enroll','--server',base,'--device-id',device['id']],
            input=enrollment['enrollment_token']+'\n',ok=False)
        assert not CONFIG.exists()
        install = ['sh',str(ROOT/'scripts/install-linux.sh'),'--server',base,
                   '--device-id',device['id'],'--ca-file',cert,'--no-start']
        run(install,input=enrollment['enrollment_token']+'\n')
        assert stat.S_IMODE(CONFIG.stat().st_mode) == 0o600 and CONFIG.stat().st_uid == 0
        assert 'pam-auth' in Path('/etc/pam.d/common-auth').read_text()
        assert 'pam-account' in Path('/etc/pam.d/common-account').read_text()
        api('POST','/v1/enroll',{'device_id':device['id'],'enrollment_token':enrollment['enrollment_token']},401)
        alice, bob = users['alice']['linux_name'], users['bob']['linux_name']
        try:
            pwd.getpwnam(alice)
            raise AssertionError('default-deny account was provisioned')
        except KeyError:
            pass
        api('PUT',prefix+'/access',{'allowed_users':['alice']})
        # An existing local user must never be adopted, changed or deleted.
        run(['useradd','--uid','1500','--no-create-home',alice])
        run([AGENT,'sync'],ok=False)
        assert pwd.getpwnam(alice).pw_uid == 1500
        run(['userdel',alice])
        run([AGENT,'sync'])
        run(install)  # Idempotent reinstall uses the saved machine credential.
        entry = pwd.getpwnam(alice)
        assert entry.pw_uid == users['alice']['unix_id'] and entry.pw_gid == entry.pw_uid
        assert stat.S_IMODE(Path(entry.pw_dir).stat().st_mode) == 0o700
        assert os.getgrouplist(alice,entry.pw_gid) == [entry.pw_gid]
        Path('/etc/pam.d/linad-system').write_text('@include common-auth\n@include common-account\n@include common-session\n')
        def pam(login, password=None, ok=True, service='linad-system', account_only=False):
            operations = ['acct_mgmt'] if account_only else ['authenticate','acct_mgmt','open_session','close_session']
            return run(['pamtester',service,login,*operations], input=None if password is None else password+'\n', ok=ok)
        pam(alice,user_password)
        pam(alice,'wrong-password',ok=False)
        pam(alice,user_password,service='linad')
        run(['usermod','--comment','Alice Example',alice])
        run([AGENT,'sync'])
        pam(alice,user_password)
        # A real local administrator remains independent from the directory.
        run(['useradd','--create-home','localadmin'])
        run(['chpasswd'],input='localadmin:'+local_password+'\n')
        pam('localadmin',local_password)
        shell = run(['runuser','-u','localadmin','--','su','-',alice,'-c','id -u'], input=user_password+'\n')
        assert shell.stdout.strip() == str(users['alice']['unix_id'])
        # Live policy checks deny a new session immediately, before periodic sync.
        api('PUT',prefix+'/access',{'allowed_users':['bob']})
        pam(alice,account_only=True,ok=False)
        run([AGENT,'sync'])
        alice_shadow = next(line for line in Path('/etc/shadow').read_text().splitlines() if line.startswith(alice+':'))
        assert alice_shadow.split(':')[7] == '1'
        pam(bob,user_password)
        # The background process provisions newly granted group members.
        api('POST','/v1/groups',{'name':'linux-users'})
        api('PUT','/v1/groups/linux-users/members/alice')
        api('PUT',prefix+'/access',{'allowed_groups':['linux-users']})
        daemon = subprocess.Popen([AGENT,'daemon','--interval','5s'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        for _ in range(50):
            line = next(line for line in Path('/etc/shadow').read_text().splitlines() if line.startswith(alice+':'))
            if line.split(':')[7] == '':
                break
            time.sleep(.1)
        else:
            raise AssertionError('daemon did not restore granted account')
        run(install)  # Atomic binary replacement while the daemon is running.
        pam(alice,user_password)
        api('PATCH','/v1/users/alice',{'disabled':True})
        pam(alice,account_only=True,ok=False)
        api('PATCH','/v1/users/alice',{'disabled':False})
        new_password = secrets.token_urlsafe(24)
        api('POST','/v1/users/alice/password',{'password':new_password})
        pam(alice,user_password,ok=False)
        pam(alice,new_password)
        # Credential revocation is immediate and does not affect local accounts.
        api('DELETE',prefix+'/credential')
        pam(alice,account_only=True,ok=False)
        pam('localadmin',local_password)
        daemon.terminate(); assert daemon.wait(timeout=10) == 0; daemon = None
        enrollment = api('POST',prefix+'/enrollment')
        run([AGENT,'enroll','--replace','--server',base,'--device-id',device['id'],
             '--ca-file','/etc/linad-linux/ca.pem'],input=enrollment['enrollment_token']+'\n')
        run([AGENT,'sync'])
        pam(alice,new_password)
        # Credential files readable by ordinary users must not be accepted.
        CONFIG.chmod(0o644)
        pam(alice,account_only=True,ok=False)
        CONFIG.chmod(0o600)
        server.terminate(); server.wait(timeout=5)
        pam(alice,account_only=True,ok=False)
        pam('localadmin',local_password)
        # Uninstall works offline and preserves home data and recovery files.
        run(['sh',str(ROOT/'scripts/uninstall-linux.sh')])
        assert Path(entry.pw_dir).is_dir() and CONFIG.exists()
        assert 'linad-linux' not in Path('/etc/pam.d/common-auth').read_text()
        pam('localadmin',local_password)
        pam(alice,account_only=True,ok=False)
        print('PASS: real Debian PAM login/session, private homes, stable UID/GID, default deny, local-account collision protection, admin ACLs/groups, daemon sync, immediate revocation, password changes, one-use enrollment, CA validation, credential-file permissions, offline deny/local fallback, idempotent install and uninstall')
    finally:
        if daemon is not None and daemon.poll() is None:
            daemon.terminate(); daemon.wait(timeout=10)
        if server.poll() is None:
            server.terminate(); server.wait(timeout=5)
