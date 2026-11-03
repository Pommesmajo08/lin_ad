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
        # Real sudo uses a dedicated password-authenticated PAM service.
        def sudo(login, password, ok=True, login_shell=False):
            command = ['runuser','-u',login,'--','sudo','-S','-p','']
            if login_shell:
                command.append('-i')
            command += ['id','-u']
            result = run(command,input=password+'\n',ok=ok)
            if ok:
                assert result.stdout.strip() == '0'
        sudo(alice,user_password,ok=False)
        api('PATCH','/v1/users/alice',{'admin':True})
        run([AGENT,'sync'])
        policy = Path('/etc/sudoers.d/linad-linux')
        assert stat.S_IMODE(policy.stat().st_mode) == 0o440
        assert 'NOPASSWD' not in policy.read_text()
        sudo(alice,user_password)
        sudo(alice,user_password,login_shell=True)
        sudo(alice,'wrong-password',ok=False)
        api('PATCH','/v1/users/alice',{'admin':False})
        assert alice in policy.read_text()  # No sync: stale local grant still exists.
        sudo(alice,user_password,ok=False)
        pam(alice,user_password)  # Demotion must not break ordinary Linux login.
        run([AGENT,'sync'])
        assert not policy.exists()
        api('PATCH','/v1/users/alice',{'admin':True})
        run([AGENT,'sync'])
        sudo(alice,user_password)
        # Live policy checks deny a new session immediately, before periodic sync.
        api('PUT',prefix+'/access',{'allowed_users':['bob']})
        sudo(alice,user_password,ok=False)
        pam(alice,account_only=True,ok=False)
        run([AGENT,'sync'])
        alice_shadow = next(line for line in Path('/etc/shadow').read_text().splitlines() if line.startswith(alice+':'))
        assert alice_shadow.split(':')[7] == '1'
        pam(bob,user_password)
        # Automation: real execution, lost claim/result replies, crash recovery,
        # explicit resolution and a real signed-repository apt refresh.
        import http.server
        import threading
        def create_job(name, kind='system-info'):
            return api('POST','/v1/jobs',{'name':name,'kind':kind,'device_ids':[device['id']]},201)
        def job_run(job):
            return next(run for run in api('GET','/v1/job-runs') if run['job_id']==job['id'])
        inventory = create_job('System inventory')
        run([AGENT,'jobs-once'])
        result = job_run(inventory)
        assert result['status']=='succeeded' and 'Architecture:' in result['output']
        run([AGENT,'jobs-once'])
        assert len([r for r in api('GET','/v1/job-runs') if r['job_id']==inventory['id']])==1
        retried = create_job('Retry-safe inventory')
        drops = {'claim':True,'result':True,'claim_count':0,'result_count':0}
        class DropReplies(http.server.BaseHTTPRequestHandler):
            def log_message(self,*args):
                pass
            def do_POST(self):
                data=self.rfile.read(int(self.headers.get('Content-Length','0')))
                request=urllib.request.Request(base+self.path,method='POST',data=data,
                    headers={'Content-Type':'application/json','Authorization':self.headers.get('Authorization','')})
                try:
                    response=urllib.request.urlopen(request,context=context,timeout=15)
                except urllib.error.HTTPError as error:
                    response=error
                payload=response.read()
                kind='claim' if self.path.endswith('/claim') else 'result'
                drops[kind+'_count']+=1
                if drops[kind]:
                    drops[kind]=False
                    self.send_response(503);self.end_headers();self.wfile.write(b'{}');return
                self.send_response(response.status);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(payload)
        gateway=http.server.ThreadingHTTPServer(('127.0.0.1',0),DropReplies)
        thread=threading.Thread(target=gateway.serve_forever,daemon=True);thread.start()
        proxy_config=Path(tmp+'/proxy-client.json')
        configuration=json.loads(CONFIG.read_text())
        configuration['server']='http://127.0.0.1:'+str(gateway.server_port)
        configuration['allow_http']=True
        proxy_config.write_text(json.dumps(configuration));proxy_config.chmod(0o600)
        try:
            command=[AGENT,'jobs-once','--config',str(proxy_config)]
            run(command,ok=False)  # Claim committed; response lost; no local execution.
            assert job_run(retried)['status']=='running'
            run(command,ok=False)  # Same claim, one execution; result response lost.
            finished=job_run(retried)
            assert finished['status']=='succeeded'
            run(command)          # Resends cached result without re-executing.
            assert job_run(retried)['finished_at']==finished['finished_at']
            assert drops['claim_count']==2 and drops['result_count']==2
        finally:
            gateway.shutdown();gateway.server_close();thread.join(timeout=5)
        interrupted=create_job('Interrupted inventory')
        configuration=json.loads(CONFIG.read_text())
        nonce=secrets.token_hex(32)
        claim_request=urllib.request.Request(base+'/v1/machine/jobs/claim',method='POST',
            data=json.dumps({'request_id':nonce}).encode(),headers={'Content-Type':'application/json',
            'Authorization':'Device '+device['id']+'.'+configuration['secret']})
        claimed=json.load(urllib.request.urlopen(claim_request,context=context))['run']
        ledger_path=Path('/var/lib/linad-linux/jobs.json')
        ledger=json.loads(ledger_path.read_text())
        ledger['request_id']=nonce
        ledger['current']={'run':claimed,'request_id':nonce,'status':'running','exit_code':0,'output':''}
        ledger_path.write_text(json.dumps(ledger));ledger_path.chmod(0o600)
        run([AGENT,'jobs-once'])
        assert job_run(interrupted)['status']=='unknown'
        blocked=create_job('Blocked until reviewed')
        run([AGENT,'jobs-once'])
        assert job_run(blocked)['status']=='queued'
        api('POST','/v1/job-runs/'+claimed['id']+'/resolve')
        run([AGENT,'jobs-once'])
        assert job_run(blocked)['status']=='succeeded'
        refresh=create_job('Refresh package indexes','apt-refresh')
        run([AGENT,'jobs-once'])
        assert job_run(refresh)['status']=='succeeded'
        assert 'apt-get update' in job_run(refresh)['output']
        # A normal daemon stop drains an already accepted package operation.
        apt_path=Path('/usr/bin/apt-get')
        original=Path('/usr/bin/apt-get.linad-test-original')
        apt_path.rename(original)
        apt_path.write_text('#!/bin/sh\nprintf ready > /tmp/linad-job-started\nsleep 3\nexec /usr/bin/apt-get.linad-test-original "$@"\n')
        apt_path.chmod(0o755)
        try:
            draining=create_job('Drain on service stop','apt-refresh')
            daemon=subprocess.Popen([AGENT,'daemon','--interval','5s'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            for _ in range(100):
                if Path('/tmp/linad-job-started').exists():
                    break
                time.sleep(.1)
            else:
                raise AssertionError('daemon did not start package job')
            daemon.terminate()
            assert daemon.wait(timeout=30)==0
            daemon=None
            assert job_run(draining)['status']=='succeeded'
        finally:
            original.replace(apt_path)
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
        sudo(alice,new_password)
        # Credential revocation is immediate and does not affect local accounts.
        api('DELETE',prefix+'/credential')
        sudo(alice,new_password,ok=False)
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
        sudo(alice,new_password,ok=False)
        pam(alice,account_only=True,ok=False)
        pam('localadmin',local_password)
        # Uninstall works offline and preserves home data and recovery files.
        run(['sh',str(ROOT/'scripts/uninstall-linux.sh')])
        assert Path(entry.pw_dir).is_dir() and CONFIG.exists()
        assert not Path('/etc/sudoers.d/linad-linux').exists()
        assert 'linad-linux' not in Path('/etc/pam.d/common-auth').read_text()
        pam('localadmin',local_password)
        pam(alice,account_only=True,ok=False)
        print('PASS: real Linux jobs, apt refresh, lost-response retries, no duplicate execution, crash recovery/resolution, graceful daemon drain, Debian sudo/sudo-i, immediate role revocation, normal-user denial, PAM login/session, private homes, stable UID/GID, default deny, local-account collision protection, admin ACLs/groups, daemon sync, immediate revocation, password changes, one-use enrollment, CA validation, credential-file permissions, offline deny/local fallback, idempotent install and uninstall')
    finally:
        if daemon is not None and daemon.poll() is None:
            daemon.terminate(); daemon.wait(timeout=10)
        if server.poll() is None:
            server.terminate(); server.wait(timeout=5)
