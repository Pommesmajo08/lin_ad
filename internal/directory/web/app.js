'use strict';
let token = '', profile = null;
const $ = id => document.getElementById(id);
const pathName = name => encodeURIComponent(name);
function message(text, error = false) {
  $('message').textContent = text;
  $('message').className = error ? 'error' : '';
  $('message').hidden = !text;
}
function signedOut() {
  token = ''; profile = null;
  $('workspace').hidden = true; $('session').hidden = true;
  $('admin-panel').hidden = true; $('login-panel').hidden = false;
  $('users').replaceChildren(); $('groups').replaceChildren(); $('reset-user').replaceChildren();
  document.querySelectorAll('input[type=password]').forEach(input => { input.value = ''; });
}
async function api(method, path, body) {
  const headers = { 'Content-Type': 'application/json' };
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
  'administrator required': 'Dafür sind Administratorrechte erforderlich.'
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
  $('workspace').hidden = false; $('session').hidden = false; $('login-panel').hidden = true;
  if (!profile.admin) return;
  const users = await api('GET', '/v1/users');
  const groups = await api('GET', '/v1/groups');
  $('user-count').textContent = users.length;
  $('group-count').textContent = Object.keys(groups).length;
  $('users').replaceChildren(); $('reset-user').replaceChildren();
  users.forEach(user => {
    const row = element('tr');
    row.append(element('td', user.name), element('td', user.admin ? 'Administrator' : 'Benutzer'));
    const status = element('td'); status.append(element('span', user.disabled ? 'Gesperrt' : 'Aktiv', user.disabled ? 'badge disabled' : 'badge')); row.append(status);
    const controls = element('td');
    if (user.name !== 'admin') {
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
