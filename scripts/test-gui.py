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
            assert page.locator('#device-form [name=name]').input_value()
            page.locator('#device-form [name=name]').fill('gui-workstation')
            page.get_by_role('button', name='Gerät registrieren', exact=True).click()
            page.get_by_text('Gerät registriert.', exact=True).wait_for()
            row = page.locator('#devices tr').filter(has=page.get_by_role('cell', name='gui-workstation', exact=True))
            expect(row).to_contain_text('admin')
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
            cli('device-register', 'cli-workstation')
            page.get_by_role('button', name='Aktualisieren', exact=True).click()
            page.get_by_text('Verzeichnis aktualisiert.', exact=True).wait_for()
            expect(page.locator('#devices tr')).to_have_count(2)
            device_id = next(device['id'] for device in cli('devices') if device['name'] == 'cli-workstation')
            assert cli('device-heartbeat', device_id)['id'] == device_id
            assert cli('device-delete', device_id)['ok']
            row.get_by_role('button', name='Entfernen', exact=True).click()
            page.get_by_text('Gerät entfernt.', exact=True).wait_for()
            assert cli('devices') == []
            page.set_viewport_size({'width':390, 'height':844})
            page.screenshot(path=tmp + '/gui-mobile.png', full_page=True)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
            assert page.evaluate('localStorage.length === 0 && sessionStorage.length === 0')
            page.get_by_role('button', name='Client beenden', exact=True).click()
            page.get_by_text('Der lokale Client wurde beendet. Du kannst diesen Tab schließen.', exact=True).wait_for()
            assert not errors, errors
            browser.close()
        assert gui.wait(timeout=10) == 0
        print('PASS: GUI executable, server selection, login, device registration/heartbeat/removal, CLI interoperability, mobile layout, local access protection and clean shutdown')
    finally:
        if gui is not None and gui.poll() is None:
            gui.terminate()
            gui.wait(timeout=5)
        server.terminate()
        server.wait(timeout=5)
