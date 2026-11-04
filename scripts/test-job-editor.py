"""Python job editing regression: build first; temporary server, Chromium, port 18084."""
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time
import urllib.request
from playwright.sync_api import sync_playwright, expect

root = Path(__file__).resolve().parents[1]
base = 'http://127.0.0.1:18084'
with tempfile.TemporaryDirectory() as tmp:
    password = secrets.token_urlsafe(24)
    env = dict(os.environ, LINAD_ADMIN_PASSWORD=password, LINAD_DATA=tmp+'/db.json', LINAD_LISTEN='127.0.0.1:18084')
    server = subprocess.Popen([str(root/'dist/linad-server')], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(50):
            try:
                urllib.request.urlopen(base+'/healthz', timeout=1)
                break
            except Exception:
                time.sleep(.1)
        else:
            raise RuntimeError('server not ready')
        def api(method, path, body=None, token=''):
            headers={'Content-Type':'application/json', 'Authorization':'Bearer '+token}
            return json.load(urllib.request.urlopen(urllib.request.Request(base+path, method=method, headers=headers, data=None if body is None else json.dumps(body).encode())))
        token=api('POST','/v1/login',{'name':'admin','password':password})['token']
        device=api('POST','/v1/devices',{'name':'python-test','os':'linux'},token)
        with sync_playwright() as p:
            browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
            page=browser.new_page(viewport={'width':1440,'height':1000})
            errors=[]
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.goto(base)
            page.locator('#login-form [name=name]').fill('admin')
            page.locator('#login-form [name=password]').fill(password)
            page.get_by_role('button',name='Anmelden',exact=True).click()
            page.get_by_text('Erfolgreich angemeldet.',exact=True).wait_for()
            page.locator('[data-view=automation]').click()
            expect(page.locator('[data-page=devices]')).to_be_hidden()
            expect(page.locator('[data-page=accounts]')).to_be_hidden()
            page.locator('#job-create > summary').click()
            page.locator('#job-form [name=name]').fill('Python workflow')
            page.locator('#job-form [name=kind]').select_option('custom-script')
            page.locator('#job-form [name=interpreter]').select_option('python3')
            source="print('Grüße von Python')\n"
            page.locator('#job-form [name=script]').fill(source)
            # Missing devices must produce visible validation feedback, not a silent button.
            page.get_by_role('button',name='Job anlegen',exact=True).click()
            expect(page.locator('#message')).to_have_class('error')
            page.locator('#job-devices').select_option(device['id'])
            with page.expect_response(lambda r: r.request.method=='POST' and r.url.endswith('/v1/jobs')) as response:
                page.get_by_role('button',name='Job anlegen',exact=True).click()
            assert response.value.status==201
            page.get_by_text('Job angelegt.',exact=True).wait_for()
            job=api('GET','/v1/jobs',token=token)[0]
            assert api('GET','/v1/jobs/'+job['id'],token=token)['interpreter']=='python3'
            card=page.locator('.job-card')
            card.get_by_role('button',name='Skript anzeigen / bearbeiten',exact=True).click()
            editor=card.locator('.script-editor')
            expect(editor.locator('select')).to_have_value('python3')
            replacement="print('geändert')\n"
            editor.locator('textarea').fill(replacement)
            # Failed saves retain the draft and show an error directly in the editor.
            pattern='**/v1/jobs/'+job['id']
            def fail_patch(route):
                if route.request.method=='PATCH':
                    route.fulfill(status=500,content_type='application/json',body='{"error":"job persistence failed"}')
                else:
                    route.continue_()
            page.route(pattern,fail_patch)
            editor.get_by_role('button',name='Skript speichern',exact=True).click()
            expect(editor.locator('.form-feedback')).to_be_visible()
            expect(editor.locator('textarea')).to_have_value(replacement)
            expect(editor.get_by_role('button',name='Skript speichern',exact=True)).to_be_enabled()
            page.unroute(pattern,fail_patch)
            with page.expect_response(lambda r: r.request.method=='PATCH' and r.url.endswith('/v1/jobs/'+job['id'])) as response:
                editor.get_by_role('button',name='Skript speichern',exact=True).click()
            assert response.value.status==200
            page.get_by_text('Skript für neue Läufe gespeichert.',exact=True).wait_for()
            detail=api('GET','/v1/jobs/'+job['id'],token=token)
            assert detail['script']==replacement and detail['interpreter']=='python3'
            # Switching areas does not discard an unsaved draft.
            card.get_by_role('button',name='Skript anzeigen / bearbeiten',exact=True).click()
            editor.locator('textarea').fill('print("draft")\n')
            page.locator('[data-view=account]').click()
            page.locator('[data-view=automation]').click()
            expect(editor.locator('textarea')).to_have_value('print("draft")\n')
            for theme in ['light','dark']:
                page.locator('#theme').select_option(theme)
                for width in [1440,390]:
                    page.set_viewport_size({'width':width,'height':900})
                    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                    page.screenshot(path='/tmp/linad-jobs-'+theme+'-'+str(width)+'.png',full_page=True)
            assert not errors, errors
            browser.close()
        print('PASS: Python creation/edit/save, visible validation, failed save/retry with preserved draft, navigation, light/dark desktop/mobile')
    finally:
        server.terminate()
        server.wait(timeout=5)
