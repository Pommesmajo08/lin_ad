"""Build and exercise the server image; optionally export with --export-image.
All state and credentials belong to an ephemeral test container on a random loopback port.
"""
import argparse
import json
from pathlib import Path
import secrets
import subprocess
import time
import tarfile
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--export-image', action='store_true')
args = parser.parse_args()

def docker(*arguments):
    return subprocess.check_output(['docker', *arguments], text=True).strip()

name = 'linad-container-smoke-'+secrets.token_hex(6)
password = secrets.token_urlsafe(32)
base = ''
docker('build', '-t', 'linad-server:local', str(root))
docker('run', '-d', '--name', name, '-p', '127.0.0.1::8080',
       '-e', 'LINAD_ADMIN_PASSWORD='+password, 'linad-server:local')
try:
    port=docker('port',name,'8080/tcp').rsplit(':',1)[1]
    base='http://127.0.0.1:'+port
    def ready():
        global base
        port=docker('port',name,'8080/tcp').rsplit(':',1)[1]
        base='http://127.0.0.1:'+port
        for _ in range(100):
            try:
                assert json.load(urllib.request.urlopen(base+'/healthz',timeout=1))['status']=='ok'
                return
            except Exception:
                time.sleep(.1)
        raise AssertionError('container did not become ready')
    ready()
    def api(method,path,auth='',body=None,want=200):
        headers={'Content-Type':'application/json'}
        if auth:
            headers['Authorization']=auth
        request=urllib.request.Request(base+path,method=method,headers=headers,
                                      data=None if body is None else json.dumps(body).encode())
        try:
            response=urllib.request.urlopen(request,timeout=10)
        except urllib.error.HTTPError as error:
            response=error
        assert response.status==want,(method,path,response.status)
        return json.load(response)
    def login():
        return 'Bearer '+api('POST','/v1/login',body={'name':'admin','password':password})['token']
    admin=login()
    assert b'login-form' in urllib.request.urlopen(base+'/').read()
    user_password=secrets.token_urlsafe(24)
    api('POST','/v1/users',admin,{'name':'alice','password':user_password})
    alice='Bearer '+api('POST','/v1/login',body={'name':'alice','password':user_password})['token']
    api('POST','/v1/devices',alice,{'name':'forbidden','os':'linux'},403)
    device=api('POST','/v1/devices',admin,{'name':'container-pc','os':'linux'},201)
    prefix='/v1/devices/'+device['id']
    api('PUT',prefix+'/access',admin,{'allowed_users':['alice']})
    enrollment=api('POST',prefix+'/enrollment',admin)
    credential=api('POST','/v1/enroll',body={'device_id':device['id'], 'enrollment_token':enrollment['enrollment_token']})
    machine='Device '+device['id']+'.'+credential['secret']
    manifest=api('GET','/v1/machine/users',machine)
    assert len(manifest)==1 and manifest[0]['name']=='alice'
    assert manifest[0]['uid']>=60001
    docker('restart',name)
    ready()
    assert api('GET','/v1/machine/users',machine)==manifest
    api('DELETE',prefix+'/credential',login())
    api('GET','/v1/machine/users',machine,want=401)
    if args.export_image:
        docker('save','-o',str(root/'dist/linad-server-image.tar'),'linad-server:local')
        with tarfile.open(root/'dist/linad-server-docker.tar.gz','w:gz') as archive:
            for name in ['dist/linad-server-image.tar','scripts/install-server.sh','docs/installation.md']:
                archive.add(root/name,arcname=name)
    print('PASS: server image, web assets, admin-only device registration, ACLs, enrollment, machine identity, restart persistence and credential revocation')
finally:
    docker('rm','-fv',name)
