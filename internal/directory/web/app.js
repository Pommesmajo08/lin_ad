'use strict';
let token = '', profile = null;
let jobDeviceCache = [];
const localSecret = /^#gui=[a-f0-9]{64}$/.test(location.hash) ? location.hash.slice(5) : '';
let guiConnected = !localSecret;
const $ = id => document.getElementById(id);
const pathName = name => encodeURIComponent(name);
let activeView = 'devices';
function showView(view) {
  if (!profile?.admin && ['automation', 'accounts'].includes(view)) view = 'devices';
  activeView = view;
  document.querySelectorAll('[data-page]').forEach(page => { page.hidden = page.dataset.page !== view; });
  document.querySelectorAll('[data-admin-view]').forEach(button => { button.hidden = !profile?.admin; });
  document.querySelectorAll('[data-view]').forEach(button => {
    if (button.dataset.view === view) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
  });
}
$('workspace-nav').addEventListener('click', event => {
  const target = event.target.closest('[data-view]');
  if (target) showView(target.dataset.view);
});
const systemTheme = matchMedia('(prefers-color-scheme: dark)');
let themeChoice = 'system';
try { themeChoice = localStorage.getItem('linad-theme') || 'system'; } catch (_) { /* Storage can be disabled. */ }
if (!['system', 'light', 'dark'].includes(themeChoice)) themeChoice = 'system';
function applyTheme() { document.documentElement.dataset.theme = themeChoice === 'system' ? (systemTheme.matches ? 'dark' : 'light') : themeChoice; }
$('theme').value = themeChoice; applyTheme();
$('theme').addEventListener('change', () => {
  themeChoice = $('theme').value;
  try { localStorage.setItem('linad-theme', themeChoice); } catch (_) { /* Theme still works without storage. */ }
  applyTheme();
});
systemTheme.addEventListener('change', applyTheme);
function hideEnrollment() { $('enrollment-token').value = ''; $('enrollment-panel').hidden = true; }
$('close-enrollment').addEventListener('click', hideEnrollment);
function message(text, error = false) {
  $('message').textContent = text;
  $('message').className = error ? 'error' : '';
  $('message').hidden = !text;
}
function signedOut() {
  $('run-script-dialog').close(); $('run-script-source').value = ''; $('run-script-info').textContent = '';
  activeView = 'devices';
  $('job-create').open = false;
  hideEnrollment(); $('device-policies').replaceChildren(); $('device-policies').hidden = true;
  token = ''; profile = null;
  $('workspace').hidden = true; $('session').hidden = true;
  $('admin-panel').hidden = true; $('login-panel').hidden = !guiConnected;
  $('users').replaceChildren(); $('groups').replaceChildren(); $('reset-user').replaceChildren(); $('devices').replaceChildren(); $('my-groups').replaceChildren(); $('jobs').replaceChildren(); $('job-runs').replaceChildren(); $('job-devices').replaceChildren(); $('job-form').reset(); jobDeviceCache = []; syncScriptFields();
  document.querySelectorAll('input[type=password]').forEach(input => { input.value = ''; });
}
async function api(method, path, body) {
  const headers = { 'Content-Type': 'application/json' };
  if (localSecret) headers['X-Linad-Client'] = localSecret;
  if (token) headers.Authorization = `Bearer ${token}`;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 20000);
  let response, data;
  try {
    response = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), cache: 'no-store', redirect: 'error', signal: controller.signal });
    data = await response.json();
  } catch (error) {
    if (controller.signal.aborted) throw new Error('Der Server antwortet nicht rechtzeitig. Die Änderung wurde möglicherweise gespeichert; bitte den Status vor einem erneuten Versuch aktualisieren.');
    throw error;
  } finally { clearTimeout(timeout); }
  if (!response.ok) {
    if (response.status === 401 && token) {
      signedOut(); throw new Error('Deine Sitzung ist abgelaufen. Bitte erneut anmelden.');
    }
    throw new Error(translations[data.error] || data.error || `Fehler ${response.status}`);
  }
  return data;
}
const translations = {
  'invalid credentials': 'Benutzername oder Passwort ist falsch, oder das Konto ist gesperrt.',
  'current password is incorrect': 'Das aktuelle Passwort ist falsch.',
  'user exists': 'Dieser Benutzer existiert bereits.',
  'group exists': 'Diese Gruppe existiert bereits.',
  'login rate limit exceeded': 'Zu viele Anmeldeversuche. Bitte nach einer Minute erneut versuchen.',
  'password change rate limit exceeded': 'Zu viele Passwortversuche. Bitte nach einer Minute erneut versuchen.',
  'directory persistence failed': 'Die Änderung konnte nicht gespeichert werden.',
  'job persistence failed': 'Der Server konnte den Job nicht speichern. Deine Eingabe bleibt erhalten; bitte erneut versuchen.',
  'assign between 1 and 100 Linux devices': 'Bitte mindestens ein und höchstens 100 Linux-Geräte zuweisen.',
  'invalid interval (0 or 5–5256000 minutes)': 'Bitte einen Abstand zwischen 5 Minuten und 10 Jahren wählen; 0 bedeutet einmalig.',
  'invalid job name, kind or interval (0 or 5–5256000 minutes)': 'Bitte Namen, Aktion und Wiederholung prüfen: einmalig (0) oder mindestens 5 Minuten.',
  'administrator required': 'Dafür sind Administratorrechte erforderlich.',
  'a run is already pending for this job and device': 'Für diesen Job wartet oder läuft bereits eine Ausführung. Unklare Läufe zuerst prüfen und freigeben.',
  'only queued runs can be cancelled': 'Dieser Lauf hat bereits begonnen und wird nicht während einer Paketoperation abgebrochen.',
  'job not found': 'Dieser Job existiert nicht mehr.'
};
async function action(callback, form = document.activeElement?.closest('form')) {
  if (document.body.dataset.busy) { message('Eine Anfrage läuft noch. Bitte kurz warten.'); return; }
  document.body.dataset.busy = 'true';
  if (form) {
    form.setAttribute('aria-busy', 'true'); form.querySelector('.form-feedback')?.remove();
    const progress = element('p', 'Anfrage wird verarbeitet …', 'form-feedback pending'); progress.setAttribute('role', 'status'); form.append(progress);
  }
  document.querySelectorAll('button').forEach(button => { button.disabled = true; });
  try { await callback(); }
  catch (error) {
    const text = error.message || 'Die Verbindung zum Server ist fehlgeschlagen.';
    message(text, true);
    form?.querySelector('.form-feedback.pending')?.remove();
    if (form?.isConnected) { const feedback = element('p', text, 'form-feedback error'); feedback.setAttribute('role', 'alert'); form.append(feedback); }
  }
  finally {
    delete document.body.dataset.busy;
    form?.removeAttribute('aria-busy');
    form?.querySelector('.form-feedback.pending')?.remove();
    document.querySelectorAll('button').forEach(button => { button.disabled = false; });
  }
}
function onForm(id, callback) {
  $(id).addEventListener('submit', event => {
    event.preventDefault();
    const form = event.currentTarget;
    action(async () => { await callback(Object.fromEntries(new FormData(form)), form); }, form);
  });
}
function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}
function button(text, callback, className = 'secondary') {
  const node = element('button', text, className); node.type = 'button';
  node.addEventListener('click', () => action(callback));
  return node;
}
async function mutate(method, path, body, success) {
  await api(method, path, body);
  await refresh(); message(success);
}
async function refresh() {
  profile = await api('GET', '/v1/me');
  showView(activeView);
  $('identity').textContent = profile.name;
  $('welcome').textContent = profile.admin ? 'Verzeichnisübersicht' : `Hallo, ${profile.name}.`;
  $('admin-panel').hidden = !profile.admin;
  $('device-form').hidden = !profile.admin;
  $('linux-login').textContent = `Linux-Login: ${profile.linux_name || 'noch nicht zugewiesen'}`;
  $('workspace').hidden = false; $('session').hidden = false; $('login-panel').hidden = true;
  const ownGroups = await api('GET', '/v1/me/groups');
  $('my-groups').replaceChildren();
  ownGroups.forEach(name => $('my-groups').append(element('span', name, 'member')));
  if (!ownGroups.length) $('my-groups').append(element('span', 'Keine Gruppenmitgliedschaften.', 'muted'));
  const devices = await api('GET', '/v1/devices');
  $('device-count').textContent = `(${devices.length})`;
  $('devices').replaceChildren();
  devices.forEach(device => {
    const row = element('tr');
    row.append(element('td', device.name), element('td', device.os === 'windows' ? 'Windows' : 'Linux'), element('td', device.owner), element('td', new Date(device.last_seen).toLocaleString()));
    const controls = element('td');
    if (profile.admin) controls.append(button('Rückmeldung', () => mutate('POST', `/v1/devices/${pathName(device.id)}/heartbeat`, undefined, 'Rückmeldung gespeichert.')));
    if (profile.admin) controls.append(button('Entfernen', async () => {
      if (!confirm(`Gerät ${device.name} aus dem Verzeichnis entfernen?`)) return;
      await mutate('DELETE', `/v1/devices/${pathName(device.id)}`, undefined, 'Gerät entfernt.');
    }, 'danger'));
    if (!profile.admin) controls.append(element('span', 'Zugriff erlaubt', 'muted'));
    row.append(controls); $('devices').append(row);
  });
  if (!devices.length) {
    const row = element('tr'); const cell = element('td', 'Noch keine Geräte registriert.', 'muted'); cell.colSpan = 5; row.append(cell); $('devices').append(row);
  }
  if (!profile.admin) return;
  const users = await api('GET', '/v1/users');
  const groups = await api('GET', '/v1/groups');
  renderDevicePolicies(devices, users, groups);
  await refreshJobs();
  $('user-count').textContent = users.length;
  $('group-count').textContent = Object.keys(groups).length;
  $('users').replaceChildren(); $('reset-user').replaceChildren();
  users.forEach(user => {
    const row = element('tr');
    row.append(element('td', user.name), element('td', user.linux_name), element('td', user.admin ? 'Administrator' : 'Benutzer'));
    const status = element('td'); status.append(element('span', user.disabled ? 'Gesperrt' : 'Aktiv', user.disabled ? 'badge disabled' : 'badge')); row.append(status);
    const controls = element('td');
    if (user.name !== 'admin') {
      controls.append(button(user.admin ? 'Adminrechte entziehen' : 'Zum Administrator', async () => {
        const note = user.admin ? 'Administratorrechte und von Linad vergebene sudo-Rechte entziehen' : 'Administratorrechte und sudo auf freigegebenen Linux-Geräten erteilen';
        if (!confirm(`${user.name}: ${note}? Bestehende Websitzungen werden beendet.`)) return;
        await api('PATCH', `/v1/users/${pathName(user.name)}`, { admin: !user.admin });
        if (profile.name === user.name) { signedOut(); message('Rolle geändert. Bitte erneut anmelden.'); }
        else { await refresh(); message('Administratorrolle gespeichert.'); }
      }));
      controls.append(button(user.disabled ? 'Entsperren' : 'Sperren', async () => {
        if (!confirm(`${user.name} ${user.disabled ? 'entsperren' : 'sperren und alle Sitzungen beenden'}?`)) return;
        await mutate('PATCH', `/v1/users/${pathName(user.name)}`, { disabled: !user.disabled }, 'Benutzerstatus gespeichert.');
      }));
      controls.append(button('Löschen', async () => {
        if (!confirm(`Benutzer ${user.name} und alle seine Mitgliedschaften endgültig löschen?`)) return;
        await mutate('DELETE', `/v1/users/${pathName(user.name)}`, undefined, 'Benutzer gelöscht.');
      }, 'danger'));
    }
    row.append(controls); $('users').append(row);
    const option = element('option', user.name); option.value = user.name; $('reset-user').append(option);
  });
  $('groups').replaceChildren();
  if (!Object.keys(groups).length) $('groups').append(element('p', 'Noch keine Gruppen vorhanden.', 'muted'));
  Object.keys(groups).sort().forEach(name => {
    const group = element('div', undefined, 'group');
    const heading = element('div', undefined, 'group-title');
    heading.append(element('h3', name), button('Gruppe löschen', async () => {
      if (!confirm(`Gruppe ${name} endgültig löschen?`)) return;
      await mutate('DELETE', `/v1/groups/${pathName(name)}`, undefined, 'Gruppe gelöscht.');
    }, 'danger')); group.append(heading);
    const members = element('div', undefined, 'members');
    [...groups[name]].sort().forEach(member => {
      const chip = element('span', undefined, 'member'); chip.append(element('span', member));
      const remove = button('×', () => mutate('DELETE', `/v1/groups/${pathName(name)}/members/${pathName(member)}`, undefined, 'Mitglied entfernt.'));
      remove.setAttribute('aria-label', `${member} aus ${name} entfernen`); chip.append(remove); members.append(chip);
    });
    if (!groups[name].length) members.append(element('span', 'Keine Mitglieder', 'muted'));
    group.append(members);
    const available = users.filter(user => !groups[name].includes(user.name));
    if (available.length) {
      const form = element('form', undefined, 'inline');
      const label = element('label', 'Mitglied hinzufügen'); const select = element('select');
      available.forEach(user => { const option = element('option', user.name); option.value = user.name; select.append(option); });
      label.append(select); const add = element('button', 'Hinzufügen'); add.type = 'submit'; form.append(label, add);
      form.addEventListener('submit', event => { event.preventDefault(); action(() => mutate('PUT', `/v1/groups/${pathName(name)}/members/${pathName(select.value)}`, undefined, 'Mitglied hinzugefügt.')); });
      group.append(form);
    }
    $('groups').append(group);
  });
}
onForm('login-form', async (data, form) => {
  const response = await api('POST', '/v1/login', data); token = response.token;
  form.reset(); await refresh(); message('Erfolgreich angemeldet.');
});
onForm('user-form', async (data, form) => { await api('POST', '/v1/users', data); form.reset(); await refresh(); message('Benutzer angelegt.'); });
onForm('group-form', async (data, form) => { await api('POST', '/v1/groups', data); form.reset(); await refresh(); message('Gruppe angelegt.'); });
onForm('reset-form', async (data, form) => {
  if (!confirm(`Passwort für ${data.name} zurücksetzen und alle Sitzungen beenden?`)) return;
  await api('POST', `/v1/users/${pathName(data.name)}/password`, { password: data.password });
  form.reset();
  if (data.name === profile.name) { signedOut(); message('Passwort geändert. Bitte erneut anmelden.'); }
  else { await refresh(); message('Passwort zurückgesetzt.'); }
});
onForm('password-form', async (data, form) => {
  if (data.new_password !== data.repeat) throw new Error('Die neuen Passwörter stimmen nicht überein.');
  await api('POST', '/v1/password', { current_password: data.current_password, new_password: data.new_password });
  form.reset(); signedOut(); message('Passwort geändert. Bitte mit dem neuen Passwort anmelden.');
});
$('logout').addEventListener('click', () => action(async () => {
  try { await api('POST', '/v1/logout'); }
  finally { signedOut(); }
  message('Du bist abgemeldet.');
}));
$('refresh').addEventListener('click', () => action(async () => { await refresh(); message('Verzeichnis aktualisiert.'); }));

onForm('device-form', async (data, form) => {
  await api('POST', '/v1/devices', data); form.reset(); await refresh(); message('Gerät registriert.');
});
onForm('connection-form', async (data) => {
  await api('POST', '/client/connect', { server: data.server, allow_http: data.allow_http === 'on' });
  guiConnected = true; signedOut();
  $('connection-panel').hidden = true;
  $('server-address').textContent = data.server;
  message('Server ausgewählt. Bitte anmelden.');
});
$('quit-client').addEventListener('click', () => action(async () => {
  if (token) { try { await api('POST', '/v1/logout'); } catch (_) { /* Always permit local shutdown. */ } }
  await api('POST', '/client/quit'); signedOut(); guiConnected = false;
  $('login-panel').hidden = true; $('local-client').hidden = true;
  message('Der lokale Client wurde beendet. Du kannst diesen Tab schließen.');
}));
if (localSecret) {
  $('login-panel').hidden = true; $('local-client').hidden = false;
  action(async () => {
    const info = await api('GET', '/client/info');
    guiConnected = !!info.server;
    $('connection-panel').hidden = guiConnected;
    $('login-panel').hidden = !guiConnected;
    $('server-address').textContent = info.server || 'Noch kein Server ausgewählt';
    const nameInput = $('device-form').elements.name;
    nameInput.defaultValue = info.name;
    $('device-form').elements.os.value = info.os === 'windows' ? 'windows' : 'linux';
    for (const option of $('device-form').elements.os.options) option.defaultSelected = option.value === info.os;
  });
}

function renderDevicePolicies(devices, users, groups) {
  const area = $('device-policies'); area.replaceChildren(); area.hidden = false;
  devices.forEach(device => {
    const card = element('section', undefined, 'card device-policy');
    card.append(element('h2', `Anmeldezugriff · ${device.name}`));
    const state = device.provisioned ? 'Geräte-Schlüssel aktiv' : 'Noch nicht provisioniert';
    card.append(element('p', `${device.os === 'linux' ? state : 'Windows: nur Inventar'} · ID ${device.id}`, 'muted fine'));
    const form = element('form', undefined, 'policy-form');
    const allLabel = element('label', undefined, 'checkbox');
    const all = element('input'); all.type = 'checkbox'; all.name = 'allow_all'; all.checked = device.allow_all;
    allLabel.append(all, element('span', 'Alle aktiven Benutzer erlauben')); form.append(allLabel);
    function selection(title, values, selected) {
      const label = element('label', title); const select = element('select'); select.multiple = true; select.size = 5;
      values.forEach(([name, text]) => { const option = element('option', text); option.value = name; option.selected = selected.includes(name); select.append(option); });
      label.append(select); form.append(label); return select;
    }
    const userSelect = selection('Einzelne Benutzer', users.map(user => [user.name, `${user.name}${user.disabled ? ' (gesperrt)' : ''} → ${user.linux_name}`]), device.allowed_users || []);
    const groupSelect = selection('Gruppen', Object.keys(groups).sort().map(name => [name, name]), device.allowed_groups || []);
    const save = element('button', 'Anmeldezugriff speichern'); save.type = 'submit'; form.append(save);
    form.addEventListener('submit', event => {
      event.preventDefault(); action(() => mutate('PUT', `/v1/devices/${pathName(device.id)}/access`, {
        allow_all: all.checked,
        allowed_users: [...userSelect.selectedOptions].map(option => option.value),
        allowed_groups: [...groupSelect.selectedOptions].map(option => option.value)
      }, 'Anmeldezugriff gespeichert.'));
    });
    card.append(form, element('p', 'Mehrfachauswahl mit Strg/Cmd. Ohne Freigabe darf sich niemand anmelden. Gesperrte Konten bleiben immer ausgeschlossen.', 'muted fine'));
    if (device.os === 'linux') {
      const actions = element('div', undefined, 'policy-actions');
      actions.append(button('Linux provisionieren', async () => {
        const enrollment = await api('POST', `/v1/devices/${pathName(device.id)}/enrollment`);
        $('enrollment-device').textContent = `${device.name} · Geräte-ID: ${device.id}`;
        $('enrollment-token').value = enrollment.enrollment_token;
        $('enrollment-expiry').textContent = `Gültig bis ${new Date(enrollment.expires_at).toLocaleString()}`;
        $('enrollment-panel').hidden = false; $('enrollment-panel').scrollIntoView({ behavior: 'smooth', block: 'center' });
      }));
      actions.append(button('Gerätezugang widerrufen', async () => {
        if (!confirm(`Geräte-Schlüssel für ${device.name} widerrufen? Neue Linux-Anmeldungen werden damit gesperrt.`)) return;
        await api('DELETE', `/v1/devices/${pathName(device.id)}/credential`);
        hideEnrollment(); await refresh(); message('Gerätezugang widerrufen.');
      }, 'danger'));
      card.append(actions);
    }
    area.append(card);
  });
}


const jobKindLabels = { 'system-info': 'Systeminformationen', 'apt-refresh': 'Paketlisten aktualisieren', 'apt-upgrade': 'Paketupdates installieren', 'custom-script': 'Eigenes Root-Skript' };
const jobStatusLabels = { queued: 'Wartet', running: 'Läuft', succeeded: 'Erfolgreich', failed: 'Fehlgeschlagen', unknown: 'Unklar', cancelled: 'Vor Start abgebrochen' };
const intervalUnits = [[525600, 'Jahre (365 Tage)', 'Jahr', 'Jahre'], [10080, 'Wochen', 'Woche', 'Wochen'], [1440, 'Tage', 'Tag', 'Tage'], [60, 'Stunden', 'Stunde', 'Stunden'], [1, 'Minuten', 'Minute', 'Minuten']];
function splitInterval(minutes) {
  const unit = minutes ? intervalUnits.find(([factor]) => minutes % factor === 0)[0] : 1;
  return { count: minutes / unit, unit };
}
function intervalText(minutes) {
  if (!minutes) return 'einmalig';
  const { count, unit } = splitInterval(minutes);
  const entry = intervalUnits.find(([factor]) => factor === unit);
  return count === 1 ? ({ 1: 'jede Minute', 60: 'jede Stunde', 1440: 'jeden Tag', 10080: 'jede Woche', 525600: 'jedes Jahr (365 Tage)' }[unit]) : `alle ${count} ${entry[3]}`;
}
function intervalMinutes(count, unit) {
  const minutes = Number(count) * Number(unit);
  if (!intervalUnits.some(([factor]) => factor === Number(unit)) || !Number.isInteger(Number(count)) || !Number.isSafeInteger(minutes) || minutes < 0 || (minutes > 0 && minutes < 5) || minutes > 5256000) throw new Error('Wiederholung: 0 für einmalig oder ein Abstand zwischen 5 Minuten und 10 Jahren. Bitte eine ganze Zahl eingeben.');
  return minutes;
}
function jobDeviceName(id) { return jobDeviceCache.find(device => device.id === id)?.name || id; }
function jobDate(value) { return value ? new Date(value).toLocaleString() : '—'; }
async function refreshJobs() {
  const devices = await api('GET', '/v1/devices');
  jobDeviceCache = devices.filter(device => device.os === 'linux');
  const selectedJobDevices = [...$('job-devices').selectedOptions].map(option => option.value);
  $('job-devices').replaceChildren();
  jobDeviceCache.forEach(device => { const option = element('option', device.name + (!device.provisioned ? ' (noch nicht provisioniert)' : !device.last_job_poll ? ' (noch kein Jobabruf)' : '')); option.value = device.id; option.selected = selectedJobDevices.includes(device.id); $('job-devices').append(option); });

  const jobs = await api('GET', '/v1/jobs');
  $('jobs').replaceChildren();
  if (!jobs.length) $('jobs').append(element('p', 'Noch keine Jobs angelegt.', 'muted'));
  jobs.forEach(job => {
    const card = element('div', undefined, 'job-card');
    card.append(element('h3', job.name));
    card.append(element('p', `${jobKindLabels[job.kind]} · ${job.enabled ? 'Zeitplan aktiv' : !job.interval_minutes && !job.next_run_at ? 'Einmaljob ausgelöst' : 'Zeitplan pausiert'} · ${intervalText(job.interval_minutes)}`, 'muted'));
    card.append(element('p', `Nächster Start: ${job.next_run_at ? jobDate(job.next_run_at) : 'Kein weiterer Start geplant'} · Erstellt von ${job.created_by}`, 'muted fine'));
    const settings = element('details', undefined, 'job-settings'); settings.append(element('summary', 'Geräte und Zeitplan bearbeiten')); card.append(settings);
    const form = element('form', undefined, 'inline'); const label = element('label', 'Zugewiesene Geräte'); const select = element('select'); select.multiple = true; select.size = 3;
    jobDeviceCache.forEach(device => { const option = element('option', device.name); option.value = device.id; option.selected = job.device_ids.includes(device.id); select.append(option); });
    select.required = true; label.append(select); const save = element('button', 'Gerätezuordnung speichern'); save.type = 'submit'; form.append(label, save);
    form.addEventListener('submit', event => { event.preventDefault(); action(async () => { await api('PATCH', `/v1/jobs/${pathName(job.id)}`, { device_ids: [...select.selectedOptions].map(option => option.value) }); await refreshJobs(); message('Gerätezuordnung gespeichert.'); }, form); }); settings.append(form);
    if (job.kind === 'custom-script') {
      card.append(element('p', `${job.interpreter} · SHA-256: ${job.script_sha256}`, 'muted fine'));
      const editorArea = element('div');
      card.append(button('Skript anzeigen / bearbeiten', async () => {
        if (editorArea.childNodes.length) { editorArea.replaceChildren(); return; }
        const detail = await api('GET', `/v1/jobs/${pathName(job.id)}`);
        const editor = element('form', undefined, 'script-editor');
        const label = element('label', 'Interpreter'); const interpreter = element('select');
        ['bash', 'sh', 'python3'].forEach(value => { const option = element('option', { bash: 'Bash', sh: 'POSIX sh', python3: 'Python 3' }[value]); option.value = value; option.selected = value === detail.interpreter; interpreter.append(option); }); label.append(interpreter);
        const scriptLabel = element('label', 'Skript für künftige Läufe'); const source = element('textarea'); source.required = true; source.rows = 12; source.spellcheck = false; source.value = detail.script; scriptLabel.append(source);
        const save = element('button', 'Skript speichern'); save.type = 'submit';
        editor.append(label, scriptLabel, element('p', 'Ausführung als Root. Bereits eingereihte und laufende Jobs behalten ihre bisherige Skriptversion.', 'script-warning'), save);
        editor.addEventListener('submit', event => { event.preventDefault(); action(async () => { validateScriptSource(source.value); await api('PATCH', `/v1/jobs/${pathName(job.id)}`, { script: source.value, interpreter: interpreter.value }); await refreshJobs(); message('Skript für neue Läufe gespeichert.'); }, editor); }); editorArea.append(editor);
      })); card.append(editorArea);
    }
    const schedule = element('form', undefined, 'inline');
    const startLabel = element('label', 'Nächste Startzeit (lokal)'); const startInput = element('input'); startInput.type = 'datetime-local';
    if (job.next_run_at) { const date = new Date(job.next_run_at); const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000); startInput.value = local.toISOString().slice(0, 16); }
    startLabel.append(startInput);
    const currentInterval = splitInterval(job.interval_minutes);
    const intervalLabel = element('label', 'Abstand (0 = einmalig)'); const interval = element('input'); interval.type = 'number'; interval.min = '0'; interval.max = '5256000'; interval.step = '1'; interval.value = String(currentInterval.count); interval.required = true; intervalLabel.append(interval);
    const unitLabel = element('label', 'Zeiteinheit'); const unit = element('select'); unit.name = 'interval_unit';
    [...intervalUnits].reverse().forEach(([factor, title]) => { const option = element('option', title); option.value = String(factor); option.selected = factor === currentInterval.unit; unit.append(option); }); unitLabel.append(unit);
    const saveSchedule = element('button', 'Zeitplan speichern'); saveSchedule.type = 'submit'; schedule.append(startLabel, intervalLabel, unitLabel, saveSchedule);
    schedule.addEventListener('submit', event => { event.preventDefault(); action(async () => { const update = { interval_minutes: intervalMinutes(interval.value, unit.value), enabled: true }; if (startInput.value) update.start_at = new Date(startInput.value).toISOString(); await api('PATCH', `/v1/jobs/${pathName(job.id)}`, update); await refreshJobs(); message('Zeitplan gespeichert.'); }, schedule); }); settings.append(schedule);
    const controls = element('div', undefined, 'policy-actions');
    controls.append(button('Jetzt ausführen', async () => { await api('POST', `/v1/jobs/${pathName(job.id)}/run`); await refreshJobs(); message('Job eingereiht. Ausführung beim nächsten Geräteabruf.'); }));
    controls.append(button(job.enabled ? 'Zeitplan pausieren' : 'Zeitplan aktivieren', async () => { await api('PATCH', `/v1/jobs/${pathName(job.id)}`, { enabled: !job.enabled }); await refreshJobs(); message('Zeitplan gespeichert.'); }));
    controls.append(button('Job löschen', async () => { if (!confirm(`Job ${job.name} löschen? Wartende Läufe werden abgebrochen; laufende Befehle bleiben aktiv.`)) return; await api('DELETE', `/v1/jobs/${pathName(job.id)}`); await refreshJobs(); message('Job gelöscht.'); }, 'danger'));
    card.append(controls); $('jobs').append(card);
  });
  await refreshJobRuns();
}
async function refreshJobRuns() {
  const runs = await api('GET', '/v1/job-runs'); const openLogs = new Set([...$('job-runs').querySelectorAll('details[open]')].map(details => details.dataset.runId)); $('job-runs').replaceChildren();
  runs.forEach(run => {
    const row = element('tr'); const name = element('td'); name.append(element('strong', run.job_name), element('div', jobDeviceName(run.device_id), 'muted fine'));
    const status = element('td'); status.append(element('span', jobStatusLabels[run.status] + (run.resolved && run.status === 'unknown' ? ' (freigegeben)' : ''), `badge job-status-${run.status}`));
    const date = element('td', `${jobDate(run.started_at)} / ${jobDate(run.finished_at)}`, 'fine'); const result = element('td');
    if (run.exit_code !== null) result.append(element('div', `Exit-Code: ${run.exit_code}`, 'fine'));
    if (run.output) { const details = element('details'); details.dataset.runId = run.id; details.open = openLogs.has(run.id); details.append(element('summary', 'Ausgabe anzeigen'), element('pre', run.output)); result.append(details); }
    const controls = element('td');
    if (run.kind === 'custom-script') controls.append(button('Skriptversion', async () => {
      const detail = await api('GET', `/v1/job-runs/${pathName(run.id)}`);
      $('run-script-title').textContent = `Skriptversion · ${run.job_name}`;
      $('run-script-info').textContent = `${detail.interpreter} · SHA-256: ${detail.script_sha256}`;
      $('run-script-source').value = detail.script;
      $('run-script-dialog').showModal();
    }));
    if (run.status === 'queued') controls.append(button('Abbrechen', async () => { await api('POST', `/v1/job-runs/${pathName(run.id)}/cancel`); await refreshJobRuns(); message('Wartender Lauf abgebrochen.'); }, 'danger'));
    if (run.status === 'unknown' && !run.resolved) controls.append(button('Nach Prüfung freigeben', async () => { if (!confirm('Gerät prüfen: Es darf kein alter Paketprozess mehr laufen. Die Freigabe erlaubt weitere Jobs und wiederholt diesen Lauf nicht. Fortfahren?')) return; await api('POST', `/v1/job-runs/${pathName(run.id)}/resolve`); await refreshJobRuns(); message('Gerät für weitere Jobs freigegeben.'); }));
    row.append(name, status, date, result, controls); $('job-runs').append(row);
  });
  if (!runs.length) { const row = element('tr'); const cell = element('td', 'Noch keine Ausführungen.', 'muted'); cell.colSpan = 5; row.append(cell); $('job-runs').append(row); }
}
onForm('job-form', async (data, form) => {
  const selected = [...form.elements.devices.selectedOptions].map(option => option.value);
  const start = data.start_at ? new Date(data.start_at).toISOString() : null;
  const body = { name: data.name, kind: data.kind, device_ids: selected, start_at: start, interval_minutes: intervalMinutes(data.interval_minutes, data.interval_unit), enabled: data.enabled === 'on' };
  if (data.kind === 'custom-script') { validateScriptSource(data.script); body.script = data.script; body.interpreter = data.interpreter; }
  await api('POST', '/v1/jobs', body);
  form.reset(); syncScriptFields(); $('job-create').open = false; await refreshJobs(); message('Job angelegt.');
});
$('jobs-refresh').addEventListener('click', () => action(async () => { await refreshJobs(); message('Jobstatus aktualisiert.'); }));
setInterval(() => {
  if (token && profile?.admin && !document.body.dataset.busy && !document.hidden) {
    refreshJobRuns().catch(error => { if (!token) message(error.message, true); });
  }
}, 15000);

function syncScriptFields() {
  const custom = $('job-form').elements.kind.value === 'custom-script';
  $('job-script-fields').hidden = !custom;
  $('job-form').elements.script.required = custom;
}
function validateScriptSource(source) {
  if (!source.trim() || source.includes('\0') || new TextEncoder().encode(source.replace(/\r\n/g, '\n')).length > 16384) throw new Error('Das Skript muss gültigen Text ohne NUL enthalten und darf höchstens 16 KiB groß sein.');
}
$('job-form').elements.kind.addEventListener('change', syncScriptFields);
$('job-script-file').addEventListener('change', event => action(async () => {
  const file = event.target.files[0]; if (!file) return;
  if (file.size > 16384) throw new Error('Die Skriptdatei darf höchstens 16 KiB groß sein.');
  const source = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer()); validateScriptSource(source);
  $('job-form').elements.script.value = source;
  if (file.name.toLowerCase().endsWith('.py')) $('job-form').elements.interpreter.value = 'python3';
}));
$('run-script-close').addEventListener('click', () => { $('run-script-dialog').close(); $('run-script-source').value = ''; });
syncScriptFields();

// Native browser validation prevents submit events; explain that outcome explicitly.
document.addEventListener('invalid', event => {
  const input = event.target;
  const label = input.labels?.[0]?.textContent.trim().split('\n')[0] || 'Eingabe';
  message(`${label}: ${input.validationMessage}`, true);
}, true);
