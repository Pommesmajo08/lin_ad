'use strict';
let token = '', profile = null;
let jobDeviceCache = [];
const localSecret = /^#gui=[a-f0-9]{64}$/.test(location.hash) ? location.hash.slice(5) : '';
let guiConnected = !localSecret;
const $ = id => document.getElementById(id);
const pathName = name => encodeURIComponent(name);
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
  hideEnrollment(); $('device-policies').replaceChildren(); $('device-policies').hidden = true;
  token = ''; profile = null;
  $('workspace').hidden = true; $('session').hidden = true;
  $('admin-panel').hidden = true; $('login-panel').hidden = !guiConnected;
  $('users').replaceChildren(); $('groups').replaceChildren(); $('reset-user').replaceChildren(); $('devices').replaceChildren(); $('my-groups').replaceChildren(); $('jobs').replaceChildren(); $('job-runs').replaceChildren(); $('job-devices').replaceChildren(); $('job-form').reset(); jobDeviceCache = [];
  document.querySelectorAll('input[type=password]').forEach(input => { input.value = ''; });
}
async function api(method, path, body) {
  const headers = { 'Content-Type': 'application/json' };
  if (localSecret) headers['X-Linad-Client'] = localSecret;
  if (token) headers.Authorization = `Bearer ${token}`;
  const response = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), cache: 'no-store', redirect: 'error' });
  const data = await response.json();
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
  'administrator required': 'Dafür sind Administratorrechte erforderlich.',
  'a run is already pending for this job and device': 'Für diesen Job wartet oder läuft bereits eine Ausführung. Unklare Läufe zuerst prüfen und freigeben.',
  'only queued runs can be cancelled': 'Dieser Lauf hat bereits begonnen und wird nicht während einer Paketoperation abgebrochen.',
  'job not found': 'Dieser Job existiert nicht mehr.'
};
async function action(callback) {
  if (document.body.dataset.busy) return;
  document.body.dataset.busy = 'true';
  document.querySelectorAll('button').forEach(button => { button.disabled = true; });
  try { await callback(); }
  catch (error) { message(error.message || 'Die Verbindung zum Server ist fehlgeschlagen.', true); }
  finally {
    delete document.body.dataset.busy;
    document.querySelectorAll('button').forEach(button => { button.disabled = false; });
  }
}
function onForm(id, callback) {
  $(id).addEventListener('submit', event => {
    event.preventDefault();
    const form = event.currentTarget;
    action(async () => { await callback(Object.fromEntries(new FormData(form)), form); });
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


const jobKindLabels = { 'system-info': 'Systeminformationen', 'apt-refresh': 'Paketlisten aktualisieren', 'apt-upgrade': 'Paketupdates installieren' };
const jobStatusLabels = { queued: 'Wartet', running: 'Läuft', succeeded: 'Erfolgreich', failed: 'Fehlgeschlagen', unknown: 'Unklar', cancelled: 'Vor Start abgebrochen' };
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
    card.append(element('p', `${jobKindLabels[job.kind]} · ${job.enabled ? 'Zeitplan aktiv' : !job.interval_minutes && !job.next_run_at ? 'Einmaljob ausgelöst' : 'Zeitplan pausiert'} · ${job.interval_minutes ? 'alle ' + job.interval_minutes + ' Minuten' : 'einmalig'}`, 'muted'));
    card.append(element('p', `Nächster Start: ${job.next_run_at ? jobDate(job.next_run_at) : 'Kein weiterer Start geplant'} · Erstellt von ${job.created_by}`, 'muted fine'));
    const form = element('form', undefined, 'inline'); const label = element('label', 'Zugewiesene Geräte'); const select = element('select'); select.multiple = true; select.size = 3;
    jobDeviceCache.forEach(device => { const option = element('option', device.name); option.value = device.id; option.selected = job.device_ids.includes(device.id); select.append(option); });
    select.required = true; label.append(select); const save = element('button', 'Gerätezuordnung speichern'); save.type = 'submit'; form.append(label, save);
    form.addEventListener('submit', event => { event.preventDefault(); action(async () => { await api('PATCH', `/v1/jobs/${pathName(job.id)}`, { device_ids: [...select.selectedOptions].map(option => option.value) }); await refreshJobs(); message('Gerätezuordnung gespeichert.'); }); }); card.append(form);
    const schedule = element('form', undefined, 'inline');
    const startLabel = element('label', 'Nächste Startzeit (lokal)'); const startInput = element('input'); startInput.type = 'datetime-local';
    if (job.next_run_at) { const date = new Date(job.next_run_at); const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000); startInput.value = local.toISOString().slice(0, 16); }
    startLabel.append(startInput);
    const intervalLabel = element('label', 'Wiederholung in Minuten (0 = einmalig)'); const interval = element('input'); interval.type = 'number'; interval.min = '0'; interval.max = '525600'; interval.value = String(job.interval_minutes); interval.required = true; intervalLabel.append(interval);
    const saveSchedule = element('button', 'Zeitplan speichern'); saveSchedule.type = 'submit'; schedule.append(startLabel, intervalLabel, saveSchedule);
    schedule.addEventListener('submit', event => { event.preventDefault(); action(async () => { const update = { interval_minutes: Number(interval.value), enabled: true }; if (startInput.value) update.start_at = new Date(startInput.value).toISOString(); await api('PATCH', `/v1/jobs/${pathName(job.id)}`, update); await refreshJobs(); message('Zeitplan gespeichert.'); }); }); card.append(schedule);
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
    if (run.status === 'queued') controls.append(button('Abbrechen', async () => { await api('POST', `/v1/job-runs/${pathName(run.id)}/cancel`); await refreshJobRuns(); message('Wartender Lauf abgebrochen.'); }, 'danger'));
    if (run.status === 'unknown' && !run.resolved) controls.append(button('Nach Prüfung freigeben', async () => { if (!confirm('Gerät prüfen: Es darf kein alter Paketprozess mehr laufen. Die Freigabe erlaubt weitere Jobs und wiederholt diesen Lauf nicht. Fortfahren?')) return; await api('POST', `/v1/job-runs/${pathName(run.id)}/resolve`); await refreshJobRuns(); message('Gerät für weitere Jobs freigegeben.'); }));
    row.append(name, status, date, result, controls); $('job-runs').append(row);
  });
  if (!runs.length) { const row = element('tr'); const cell = element('td', 'Noch keine Ausführungen.', 'muted'); cell.colSpan = 5; row.append(cell); $('job-runs').append(row); }
}
onForm('job-form', async (data, form) => {
  const selected = [...form.elements.devices.selectedOptions].map(option => option.value);
  const start = data.start_at ? new Date(data.start_at).toISOString() : null;
  await api('POST', '/v1/jobs', { name: data.name, kind: data.kind, device_ids: selected, start_at: start, interval_minutes: Number(data.interval_minutes), enabled: data.enabled === 'on' });
  form.reset(); await refreshJobs(); message('Job angelegt.');
});
$('jobs-refresh').addEventListener('click', () => action(async () => { await refreshJobs(); message('Jobstatus aktualisiert.'); }));
setInterval(() => {
  if (token && profile?.admin && !document.body.dataset.busy && !document.hidden) {
    refreshJobRuns().catch(error => { if (!token) message(error.message, true); });
  }
}, 15000);
