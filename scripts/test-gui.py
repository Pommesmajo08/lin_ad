"""GUI executable smoke test: build first; requires playwright and Chromium.
Uses only temporary data, loopback port 18083 and an ephemeral GUI port.
"""
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
BASE = 'http://127.0.0.1:18083'

with tempfile.TemporaryDirectory() as tmp:
    env = dict(os.environ, LINAD_ADMIN_PASSWORD=secrets.token_urlsafe(24),
               LINAD_DATA=tmp + '/directory.json', LINAD_LISTEN='127.0.0.1:18083')
    server = subprocess.Popen([str(ROOT / 'dist/linad-server')], env=env,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    gui = None
    try:
        for _ in range(50):
            try:
                urllib.request.urlopen(BASE + '/healthz', timeout=1)
                break
            except Exception:
                time.sleep(.1)
        else:
            raise RuntimeError('directory server did not start')
        gui = subprocess.Popen([str(ROOT / 'dist/linad-client-gui-linux-amd64'), '--no-browser'],
                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
        line = gui.stderr.readline()
        match = re.search(r'http://127\.0\.0\.1:\d+/#gui=[a-f0-9]{64}', line)
        if not match:
            raise RuntimeError('GUI did not emit its private launch URL')
        launch_url = match.group(0)
        local_base = launch_url.split('/#')[0]
        try:
            urllib.request.urlopen(local_base + '/client/info', timeout=2)
            raise AssertionError('unauthorized local access accepted')
        except urllib.error.HTTPError as error:
            assert error.code == 403
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path='/usr/bin/chromium', headless=True,
                                       args=['--no-sandbox'])
            page = browser.new_page(viewport={'width':1440, 'height':1100})
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.on('console', lambda msg: errors.append(msg.text) if msg.type == 'error' else None)
            page.on('dialog', lambda dialog: dialog.accept())
            page.goto(launch_url)
            page.locator('#connection-panel').wait_for(state='visible')
            page.locator('#connection-form [name=server]').fill(BASE)
            page.locator('#connection-form [name=allow_http]').check()
            page.get_by_role('button', name='Verbinden', exact=True).click()
            page.get_by_text('Server ausgewählt. Bitte anmelden.', exact=True).wait_for()
            page.locator('#login-form [name=name]').fill('admin')
            page.locator('#login-form [name=password]').fill(env['LINAD_ADMIN_PASSWORD'])
            page.get_by_role('button', name='Anmelden', exact=True).click()
            page.get_by_text('Erfolgreich angemeldet.', exact=True).wait_for()
            page.locator('#user-form [name=name]').fill('alice')
            page.locator('#user-form [name=password]').fill(secrets.token_urlsafe(24))
            page.get_by_role('button', name='Benutzer anlegen', exact=True).click()
            page.get_by_text('Benutzer angelegt.', exact=True).wait_for()
            page.locator('#theme').select_option('dark')
            expect(page.locator('html')).to_have_attribute('data-theme','dark')
            assert page.locator('#device-form [name=name]').input_value()
            page.locator('#device-form [name=name]').fill('gui-workstation')
            page.get_by_role('button', name='Gerät registrieren', exact=True).click()
            page.get_by_text('Gerät registriert.', exact=True).wait_for()
            row = page.locator('#devices tr').filter(has=page.get_by_role('cell', name='gui-workstation', exact=True))
            expect(row).to_contain_text('admin')
            policy = page.locator('.device-policy').filter(has=page.get_by_role('heading', name='Anmeldezugriff · gui-workstation', exact=True))
            policy.locator('select').nth(0).select_option('alice')
            policy.get_by_role('button', name='Anmeldezugriff speichern', exact=True).click()
            page.get_by_text('Anmeldezugriff gespeichert.', exact=True).wait_for()
            policy.get_by_role('button', name='Linux provisionieren', exact=True).click()
            page.locator('#enrollment-panel').wait_for(state='visible')
            assert len(page.locator('#enrollment-token').input_value()) == 64
            page.get_by_role('button', name='Token verbergen', exact=True).click()
            assert page.locator('#enrollment-token').input_value() == ''
            row.get_by_role('button', name='Rückmeldung', exact=True).click()
            page.get_by_text('Rückmeldung gespeichert.', exact=True).wait_for()
            # Verify both clients see the same persistent directory.
            token = json.load(urllib.request.urlopen(urllib.request.Request(
                BASE + '/v1/login', data=json.dumps({'name':'admin', 'password':env['LINAD_ADMIN_PASSWORD']}).encode(),
                headers={'Content-Type':'application/json'})))['token']
            cli_env = dict(env, LINAD_TOKEN=token)
            def cli(*args):
                return json.loads(subprocess.check_output([
                    str(ROOT / 'dist/linad-client-linux-amd64'), '--server', BASE, '--allow-http', *args
                ], env=cli_env, text=True))
            devices = cli('devices')
            assert devices[0]['name'] == 'gui-workstation'
            assert devices[0]['allowed_users'] == ['alice']
            # Plan, trigger and inspect an assigned automation job through the GUI.
            from datetime import datetime, timedelta
            page.locator('#job-form [name=name]').fill('Scheduled inventory')
            page.locator('#job-devices').select_option(devices[0]['id'])
            page.locator('#job-form [name=start_at]').fill((datetime.now()+timedelta(days=1)).strftime('%Y-%m-%dT%H:%M'))
            page.locator('#job-form [name=interval_minutes]').fill('1440')
            page.get_by_role('button',name='Job anlegen',exact=True).click()
            page.get_by_text('Job angelegt.',exact=True).wait_for()
            jobcard=page.locator('.job-card').filter(has=page.get_by_role('heading',name='Scheduled inventory',exact=True))
            jobs=cli('jobs');assert jobs[0]['interval_minutes']==1440 and jobs[0]['device_ids']==[devices[0]['id']]
            jobcard.get_by_role('button',name='Jetzt ausführen',exact=True).click()
            page.get_by_text('Job eingereiht. Ausführung beim nächsten Geräteabruf.',exact=True).wait_for()
            expect(page.locator('#job-runs')).to_contain_text('Wartet')
            enrollment=cli('device-enrollment',devices[0]['id'])
            credential=json.load(urllib.request.urlopen(urllib.request.Request(BASE+'/v1/enroll',method='POST',
                data=json.dumps({'device_id':devices[0]['id'],'enrollment_token':enrollment['enrollment_token']}).encode(),
                headers={'Content-Type':'application/json'})))
            def machine_api(path,body):
                return json.load(urllib.request.urlopen(urllib.request.Request(BASE+path,method='POST',data=json.dumps(body).encode(),
                    headers={'Content-Type':'application/json','Authorization':'Device '+devices[0]['id']+'.'+credential['secret']})))
            request_id=secrets.token_hex(32)
            claimed=machine_api('/v1/machine/jobs/claim',{'request_id':request_id})['run']
            machine_api('/v1/machine/jobs/'+claimed['id']+'/result',{'request_id':request_id,'status':'succeeded','exit_code':0,
                'output':'<script>window.badJobOutput=1</script> plain log text'})
            page.get_by_role('button',name='Jobstatus aktualisieren',exact=True).click()
            page.get_by_text('Jobstatus aktualisiert.',exact=True).wait_for()
            expect(page.locator('#job-runs')).to_contain_text('Erfolgreich')
            page.locator('#job-runs summary').click()
            expect(page.locator('#job-runs pre')).to_contain_text('<script>')
            assert page.evaluate('window.badJobOutput === undefined')
            page.get_by_role('button',name='Jobstatus aktualisieren',exact=True).click()
            expect(page.locator('#job-runs details')).to_have_attribute('open','')
            jobcard.locator('input[type=number]').fill('60')
            jobcard.get_by_role('button',name='Zeitplan speichern',exact=True).click()
            page.get_by_text('Zeitplan gespeichert.',exact=True).wait_for()
            assert cli('jobs')[0]['interval_minutes']==60
            jobcard.get_by_role('button',name='Zeitplan pausieren',exact=True).click()
            expect(jobcard).to_contain_text('Zeitplan pausiert')
            jobcard.get_by_role('button',name='Jetzt ausführen',exact=True).click()
            page.locator('#job-runs').get_by_role('button',name='Abbrechen',exact=True).click()
            page.get_by_text('Wartender Lauf abgebrochen.',exact=True).wait_for()
            assert any(r['status']=='cancelled' for r in cli('job-runs'))
            cli('device-register', 'cli-workstation')
            page.get_by_role('button', name='Aktualisieren', exact=True).click()
            page.get_by_text('Verzeichnis aktualisiert.', exact=True).wait_for()
            expect(page.locator('#devices tr')).to_have_count(2)
            assigned=[d['id'] for d in cli('devices')]
            jobcard.locator('select').select_option(assigned)
            jobcard.get_by_role('button',name='Gerätezuordnung speichern',exact=True).click()
            page.get_by_text('Gerätezuordnung gespeichert.',exact=True).wait_for()
            assert sorted(cli('jobs')[0]['device_ids'])==sorted(assigned)
            jobcard.get_by_role('button',name='Job löschen',exact=True).click()
            page.get_by_text('Job gelöscht.',exact=True).wait_for()
            assert cli('jobs')==[] and len(cli('job-runs'))==2
            # Upload and edit a script; the already queued run must retain its source.
            source="set -eu\n# <script>window.badScriptSource=1</script>\nprintf 'original\\n'\n"
            page.locator('#job-form [name=kind]').select_option('custom-script')
            expect(page.locator('#job-script-fields')).to_be_visible()
            page.locator('#job-script-file').set_input_files({'name':'custom.sh','mimeType':'text/plain','buffer':source.encode()})
            expect(page.locator('#job-form [name=script]')).to_have_value(source)
            page.locator('#job-form [name=name]').fill('Custom script')
            page.locator('#job-devices').select_option(devices[0]['id'])
            page.get_by_role('button',name='Job anlegen',exact=True).click()
            page.get_by_text('Job angelegt.',exact=True).wait_for()
            custom=cli('jobs')[0]
            assert 'script' not in custom and custom['kind']=='custom-script'
            assert cli('job-show',custom['id'])['script']==source
            card=page.locator('.job-card').filter(has=page.get_by_role('heading',name='Custom script',exact=True))
            card.get_by_role('button',name='Skript anzeigen / bearbeiten',exact=True).click()
            expect(card.locator('textarea')).to_have_value(source)
            replacement="printf 'changed\\n'\n"
            card.locator('textarea').fill(replacement)
            card.get_by_role('button',name='Skript speichern',exact=True).click()
            page.get_by_text('Skript für neue Läufe gespeichert.',exact=True).wait_for()
            assert cli('job-show',custom['id'])['script']==replacement
            nonce=secrets.token_hex(32)
            old_run=machine_api('/v1/machine/jobs/claim',{'request_id':nonce})['run']
            assert old_run['script']==source and old_run['script_sha256']==custom['script_sha256']
            machine_api('/v1/machine/jobs/'+old_run['id']+'/result',{'request_id':nonce,'status':'succeeded','exit_code':0,'output':'script-output'})
            page.get_by_role('button',name='Jobstatus aktualisieren',exact=True).click()
            page.get_by_text('Jobstatus aktualisiert.',exact=True).wait_for()
            page.locator('#job-runs').get_by_role('button',name='Skriptversion',exact=True).click()
            expect(page.locator('#run-script-dialog')).to_be_visible()
            expect(page.locator('#run-script-source')).to_have_value(source)
            assert page.evaluate('window.badScriptSource === undefined')
            page.get_by_role('button',name='Schließen',exact=True).click()
            assert page.locator('#run-script-source').input_value()==''
            card.get_by_role('button',name='Jetzt ausführen',exact=True).click()
            page.get_by_text('Job eingereiht. Ausführung beim nächsten Geräteabruf.',exact=True).wait_for()
            nonce=secrets.token_hex(32)
            updated_run=machine_api('/v1/machine/jobs/claim',{'request_id':nonce})['run']
            assert updated_run['script']==replacement
            machine_api('/v1/machine/jobs/'+updated_run['id']+'/result',{'request_id':nonce,'status':'failed','exit_code':7,'output':'expected failure'})
            page.get_by_role('button',name='Jobstatus aktualisieren',exact=True).click()
            expect(page.locator('#job-runs')).to_contain_text('Fehlgeschlagen')
            card.get_by_role('button',name='Job löschen',exact=True).click()
            page.get_by_text('Job gelöscht.',exact=True).wait_for()
            assert cli('jobs')==[] and len(cli('job-runs'))==4

            device_id = next(device['id'] for device in cli('devices') if device['name'] == 'cli-workstation')
            assert cli('device-heartbeat', device_id)['id'] == device_id
            assert cli('device-delete', device_id)['ok']
            row.get_by_role('button', name='Entfernen', exact=True).click()
            page.get_by_text('Gerät entfernt.', exact=True).wait_for()
            assert cli('devices') == []
            page.set_viewport_size({'width':390, 'height':844})
            page.screenshot(path=tmp + '/gui-mobile.png', full_page=True)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
            assert page.evaluate("Object.keys(localStorage).every(key => key === 'linad-theme') && sessionStorage.length === 0")
            page.get_by_role('button', name='Client beenden', exact=True).click()
            page.get_by_text('Der lokale Client wurde beendet. Du kannst diesen Tab schließen.', exact=True).wait_for()
            assert not errors, errors
            browser.close()
        assert gui.wait(timeout=10) == 0
        print('PASS: custom script upload/edit/immutable snapshots/source preview, GUI jobs/schedules/assignments, manual trigger/cancel/history, safe log rendering, GUI executable, server selection, login, device registration/heartbeat/removal, CLI interoperability, mobile layout, local access protection and clean shutdown')
    finally:
        if gui is not None and gui.poll() is None:
            gui.terminate()
            gui.wait(timeout=5)
        server.terminate()
        server.wait(timeout=5)
