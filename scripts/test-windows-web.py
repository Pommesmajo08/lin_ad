"""Windows agent UI regression with a temporary server; Chromium required."""
import json,os,secrets,socket,subprocess,tempfile,time,urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright,expect
root=Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as tmp:
    listener=socket.socket();listener.bind(('127.0.0.1',0));port=listener.getsockname()[1];listener.close()
    base=f'http://127.0.0.1:{port}'
    password=secrets.token_urlsafe(24)
    env=dict(os.environ,LINAD_ADMIN_PASSWORD=password,LINAD_DATA=tmp+'/db.json',LINAD_LISTEN=f'127.0.0.1:{port}')
    server=subprocess.Popen([str(root/'dist/linad-server')],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    token=''
    def api(method,path,body=None):
        request=urllib.request.Request(base+path,method=method,headers={'Content-Type':'application/json','Authorization':'Bearer '+token},data=None if body is None else json.dumps(body).encode())
        return json.load(urllib.request.urlopen(request,timeout=10))
    try:
        for _ in range(100):
            try:api('GET','/healthz');break
            except Exception:time.sleep(.1)
        token=api('POST','/v1/login',{'name':'admin','password':password})['token']
        win=api('POST','/v1/devices',{'name':'windows-pc','os':'windows'})
        linux=api('POST','/v1/devices',{'name':'linux-pc','os':'linux'})
        with sync_playwright() as p:
            browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
            page=browser.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
            page.goto(base)
            page.locator('#login-form [name=name]').fill('admin');page.locator('#login-form [name=password]').fill(password)
            page.get_by_role('button',name='Anmelden',exact=True).click();page.get_by_text('Erfolgreich angemeldet.',exact=True).wait_for()
            card=page.locator('.device-policy').filter(has=page.get_by_role('heading',name='Anmeldezugriff · windows-pc',exact=True))
            expect(card.locator('.policy-form')).to_have_count(1)
            card.get_by_role('button',name='Windows-Agent einrichten',exact=True).click()
            page.locator('#enrollment-panel').wait_for(state='visible')
            command=page.locator('#enrollment-command').input_value()
            assert 'install-agent-windows.cmd' in command and win['id'] in command and '-AllowHttp' in command,command
            assert page.locator('#enrollment-token').input_value() not in command
            page.locator('#enable-windows-logon').check()
            expect(page.locator('#enrollment-command')).to_have_value(__import__('re').compile(r'-EnableLogon$'))
            page.locator('#enable-windows-logon').uncheck()
            expect(page.locator('#enrollment-download')).to_have_attribute('href',__import__('re').compile(r'/install-agent-windows\.cmd$'))
            page.get_by_role('button',name='Token verbergen',exact=True).click()
            page.locator('[data-view=automation]').click();page.locator('#job-create > summary').click()
            page.locator('#job-form [name=kind]').select_option('custom-script')
            page.locator('#job-form [name=interpreter]').select_option('powershell')
            expect(page.locator(f'#job-devices option[value="{linux["id"]}"]')).to_have_attribute('disabled','')
            page.locator('#job-form [name=name]').fill('Windows script')
            page.locator('#job-form [name=script]').fill("Write-Output 'hello'\n")
            page.locator('#job-devices').select_option(win['id'])
            page.get_by_role('button',name='Job anlegen',exact=True).click();page.get_by_text('Job angelegt.',exact=True).wait_for()
            jobs=api('GET','/v1/jobs');assert jobs[0]['interpreter']=='powershell' and jobs[0]['device_ids']==[win['id']]
            jobcard=page.locator('.job-card');jobcard.get_by_role('button',name='Skript anzeigen / bearbeiten',exact=True).click()
            expect(jobcard.locator('.script-editor select')).to_have_value('powershell')
            jobcard.locator('textarea').fill('exit 7\n');jobcard.get_by_role('button',name='Skript speichern',exact=True).click()
            page.get_by_text('Skript für neue Läufe gespeichert.',exact=True).wait_for()
            assert api('GET','/v1/jobs/'+jobs[0]['id'])['script']=='exit 7\n'
            page.locator('#job-create > summary').click();page.locator('#job-form [name=kind]').select_option('windows-update')
            expect(page.locator('#job-script-fields')).to_be_hidden()
            expect(page.locator(f'#job-devices option[value="{linux["id"]}"]')).to_have_attribute('disabled','')
            page.locator('#job-form [name=name]').fill('Windows updates');page.locator('#job-devices').select_option(win['id'])
            with page.expect_response(lambda r:r.request.method=='POST' and r.url.endswith('/v1/jobs')):
                page.get_by_role('button',name='Job anlegen',exact=True).click()
            assert any(j['kind']=='windows-update' for j in api('GET','/v1/jobs'))
            assert not errors,errors
            browser.close()
        print('PASS: Windows enrollment/download command, Windows login ACLs/activation, OS filtering, PowerShell create/edit, Windows update assignment')
    finally:
        server.terminate();server.wait(timeout=10)
