"""Exercise the real bootstrapper and desktop installer with local release fixtures.
No GitHub calls, PAM changes or system package installations.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

root=Path(__file__).resolve().parents[1]
package='linad-client-linux-amd64.tar.gz'
with tempfile.TemporaryDirectory() as tmp:
    fixture=Path(tmp)/'fixtures'; fixture.mkdir()
    (fixture/package).write_bytes((root/'dist'/package).read_bytes())
    checksum=hashlib.sha256((fixture/package).read_bytes()).hexdigest()
    def manifest(text): (fixture/'SHA256SUMS').write_text(text)
    manifest(f'{checksum}  {package}\n')
    fake=Path(tmp)/'bin'; fake.mkdir()
    log=Path(tmp)/'downloads.jsonl'
    (fake/'curl').write_text('''#!/usr/bin/env python3
import json,os,pathlib,shutil,sys
args=sys.argv[1:]
assert '--proto' in args and args[args.index('--proto')+1]=='=https'
assert '--proto-redir' in args and args[args.index('--proto-redir')+1]=='=https'
url=args[-1]
with open(os.environ['LINAD_TEST_LOG'],'a') as log: log.write(json.dumps(args)+'\\n')
if '--config' in args:
 config=sys.stdin.read()
 assert 'ghp_LINAD_TEST_TOKEN' in config
 name={'latest':'release.json','1':'SHA256SUMS','2':'linad-client-linux-amd64.tar.gz'}.get(url.rsplit('/',1)[1],url.rsplit('/',1)[1])
 source=pathlib.Path(os.environ['LINAD_TEST_FIXTURE'])/name
 if not source.exists(): sys.exit(22)
 shutil.copyfile(source,args[args.index('--output')+1])
elif '--head' in args:
 print('https://github.com/Pommesmajo08/lin_ad/releases/tag/v0.1.0',end='')
else:
 source=pathlib.Path(os.environ['LINAD_TEST_FIXTURE'])/url.rsplit('/',1)[1]
 if not source.exists(): sys.exit(22)
 shutil.copyfile(source,args[args.index('--output')+1])
''')
    (fake/'curl').chmod(0o755)
    env=dict(os.environ,GH_TOKEN='',GITHUB_TOKEN='',PATH=str(fake)+os.pathsep+os.environ['PATH'],LINAD_TEST_LOG=str(log),LINAD_TEST_FIXTURE=str(fixture))
    def run(prefix,*args,ok=True):
        result=subprocess.run(['sh',str(root/'scripts/install-release.sh'),'client',*args,'--','--prefix',str(prefix)],env=env,capture_output=True,text=True,timeout=30)
        assert (result.returncode==0)==ok,(result.stdout,result.stderr)
        return result
    prefix=Path(tmp)/'installed client'
    run(prefix)
    assert (prefix/'bin/linad-client-gui').exists()
    run(prefix,'--version','v0.1.0')
    subprocess.run(['sh',str(root/'scripts/install-client-linux.sh'),'--prefix',str(prefix),'--uninstall'],check=True,stdout=subprocess.DEVNULL)
    assert not (prefix/'bin/linad-client-gui').exists()
    manifest('0'*64+'  '+package+'\n')
    run(prefix,'--version','v0.1.0',ok=False)
    assert not prefix.joinpath('bin/linad-client-gui').exists()
    manifest(f'{checksum}  {package}\n{checksum}  {package}\n')
    run(prefix,ok=False)
    manifest(f'{checksum}  {package}\n')
    run(prefix,'--version','../../bad',ok=False)
    (fixture/'release.json').write_text(json.dumps({'tag_name':'v0.1.0','assets':[
        {'name':'SHA256SUMS','url':'https://api.github.com/repos/Pommesmajo08/lin_ad/releases/assets/1'},
        {'name':package,'url':'https://api.github.com/repos/Pommesmajo08/lin_ad/releases/assets/2'}]}))
    env['GH_TOKEN']='ghp_LINAD_TEST_TOKEN'
    result=run(prefix)
    assert 'ghp_LINAD_TEST_TOKEN' not in result.stdout+result.stderr+log.read_text()
    assert (prefix/'bin/linad-client-gui').exists()
    env['GH_TOKEN']=''
    (fixture/'SHA256SUMS').unlink()
    run(prefix,ok=False)
    calls=[json.loads(line) for line in log.read_text().splitlines()]
    for args in calls:
        if '--head' not in args:
            assert '/releases/download/v0.1.0/' in args[-1] or args[-1].startswith('https://api.github.com/repos/Pommesmajo08/lin_ad/releases/')
            assert not Path(args[args.index('--output')+1]).parent.exists(), 'temporary download not cleaned up'
print('PASS: public/private token transport, latest/pinned release, verified download, real user install/update, bad/missing/duplicate checksums, invalid tags, TLS flags, temp cleanup')
