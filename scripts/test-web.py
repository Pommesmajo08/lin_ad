"""Optional Chromium smoke test. Requires Python playwright and /usr/bin/chromium.
Run after scripts/build.sh. Uses a temporary database and port 18082.
"""
import json, os, secrets, subprocess, tempfile, time, urllib.request
from playwright.sync_api import sync_playwright, expect
from pathlib import Path
root=str(Path(__file__).resolve().parents[1])
password=secrets.token_urlsafe(24)
userpass=secrets.token_urlsafe(24)
resetpass=secrets.token_urlsafe(24)
newpass=secrets.token_urlsafe(24)
with tempfile.TemporaryDirectory() as tmp:
 env=dict(os.environ,LINAD_ADMIN_PASSWORD=password,LINAD_DATA=tmp+'/directory.json',LINAD_LISTEN='127.0.0.1:18082')
 server=subprocess.Popen([root+'/dist/linad-server'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  for i in range(50):
   try:urllib.request.urlopen('http://127.0.0.1:18082/healthz');break
   except Exception:time.sleep(.1)
  else:raise RuntimeError('server did not start')
  with sync_playwright() as p:
   browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
   page=browser.new_page(viewport={'width':1440,'height':1100})
   errors=[]
   page.on('pageerror',lambda err:errors.append(str(err)))
   page.on('console',lambda msg:errors.append(msg.text) if msg.type=='error' else None)
   page.on('dialog',lambda d:d.accept())
   page.goto('http://127.0.0.1:18082/')
   def fill(form,name,value):page.locator('#'+form+' [name="'+name+'"]').fill(value)
   def submit(form):page.locator('#'+form+' button[type="submit"], #'+form+' button:not([type])').click()
   def login(name,pw):
    fill('login-form','name',name);fill('login-form','password',pw);submit('login-form');page.locator('#workspace').wait_for(state='visible');page.get_by_text('Erfolgreich angemeldet.',exact=True).wait_for()
   login('admin',password)
   page.locator('#theme').select_option('dark')
   expect(page.locator('html')).to_have_attribute('data-theme','dark')
   fill('user-form','name','alice');fill('user-form','password',userpass);submit('user-form');page.get_by_text('Benutzer angelegt.',exact=True).wait_for()
   fill('group-form','name','engineering');submit('group-form');page.get_by_text('Gruppe angelegt.',exact=True).wait_for()
   group=page.locator('.group').filter(has=page.get_by_role('heading',name='engineering',exact=True))
   group.locator('select').select_option('alice');group.get_by_role('button',name='Hinzufügen',exact=True).click();page.get_by_text('Mitglied hinzugefügt.',exact=True).wait_for()
   row=page.locator('#users tr').filter(has=page.get_by_role('cell',name='alice',exact=True))
   row.get_by_role('button',name='Zum Administrator',exact=True).click();page.get_by_text('Administratorrolle gespeichert.',exact=True).wait_for()
   expect(row.get_by_role('cell',name='Administrator',exact=True)).to_be_visible()
   row.get_by_role('button',name='Adminrechte entziehen',exact=True).click()
   expect(row.get_by_role('cell',name='Benutzer',exact=True)).to_be_visible()
   row.get_by_role('button',name='Sperren',exact=True).click();page.get_by_text('Benutzerstatus gespeichert.',exact=True).wait_for();assert row.locator('.badge').inner_text()=='Gesperrt'
   row.get_by_role('button',name='Entsperren',exact=True).click();expect(row.locator('.badge')).to_have_text('Aktiv')
   page.locator('#reset-user').select_option('alice');fill('reset-form','password',resetpass);submit('reset-form');page.get_by_text('Passwort zurückgesetzt.',exact=True).wait_for()
   page.screenshot(path=tmp+'/admin-desktop.png',full_page=True)
   page.get_by_role('button',name='Abmelden',exact=True).click();page.locator('#login-panel').wait_for(state='visible')
   login('alice',resetpass);assert page.locator('#admin-panel').is_hidden()
   assert page.locator('#device-form').is_hidden()
   expect(page.locator('#my-groups')).to_contain_text('engineering')
   fill('password-form','current_password',resetpass);fill('password-form','new_password',newpass);fill('password-form','repeat',newpass);submit('password-form');page.locator('#login-panel').wait_for(state='visible')
   login('alice',newpass)
   page.reload();page.locator('#login-panel').wait_for(state='visible')
   page.set_viewport_size({'width':390,'height':844})
   page.screenshot(path=tmp+'/login-mobile.png',full_page=True)
   assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
   assert page.evaluate("Object.keys(localStorage).every(key => key === 'linad-theme') && sessionStorage.length === 0")
   assert page.locator('#theme').input_value() == 'dark'
   expect(page.locator('html')).to_have_attribute('data-theme','dark')
   assert not errors,errors
   browser.close()
  print('PASS: Browser admin login, user/group creation, membership, role promotion/demotion, own groups, disable/enable, reset, normal-user password change, re-login, memory-only sessions, mobile login, no console errors')
 finally:
  server.terminate();server.wait(timeout=5)
