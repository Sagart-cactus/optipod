'use strict';
(() => {
  const el = id => document.getElementById(id);
  const message = (text, error = false) => {el('onboardingstatus').textContent = text; el('onboardingstatus').className = error ? 'error' : '';};
  const post = (path, body, headers = {}) => request(path, {method: 'POST', headers: {'Content-Type': 'application/json', ...headers}, body: JSON.stringify(body)});
  function download(name, data) {
    const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], {type: 'application/json'}));
    const a = document.createElement('a'); a.href = url; a.download = name; a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  window.closeOnboarding = () => {
    for (const id of ['onboardingdialog', 'bootstrapdialog']) if (el(id).open) el(id).close();
    el('bootstrapform').reset(); el('registrationform').reset();
    for (const id of ['registrations', 'credentials', 'adminaudit']) el(id).replaceChildren();
  };
  el('openbootstrap').onclick = () => el('bootstrapdialog').showModal();
  el('closebootstrap').onclick = () => {el('bootstrapdialog').close(); el('bootstrapform').reset();};
  el('closeonboarding').onclick = () => el('onboardingdialog').close();
  el('bootstrapform').onsubmit = async event => {
    event.preventDefault(); el('bootstraperror').textContent = '';
    const data = Object.fromEntries(new FormData(event.target));
    try {
      await post('/auth/bootstrap', {subject: data.subject, token: data.token}, {Authorization: 'Bearer ' + data.bootstrap});
      el('bootstrapform').reset(); el('bootstrapdialog').close(); await boot();
    } catch (error) {el('bootstraperror').textContent = error.message;}
  };
  async function refreshAdministration() {
    const [registrations, credentials, events] = await Promise.all([request('/api/admin/clusters'), request('/api/admin/credentials'), request('/api/admin/audit')]);
    for (const id of ['registrations', 'credentials', 'adminaudit']) el(id).replaceChildren();
    for (const r of registrations) {
      const row = node('div', undefined, 'onboardingrow');
      row.append(node('strong', r.name), node('small', `${r.id} · ${r.environment || 'No environment'} · ${r.receivedAt ? 'Last received ' + date(r.receivedAt) : 'Awaiting first agent upload'}`));
      const issue = node('button', 'Download enrollment file'); issue.disabled = r.disconnected;
      issue.onclick = async () => {
        issue.disabled = true;
        try {
          const credential = await post('/api/admin/enrollments', {clusterID: r.id});
          download(`optipod-enrollment-${r.id}.json`, {...credential, serverURL: location.origin, kubeUID: r.kubeUID});
          message('Enrollment file downloaded. It contains a secret, expires in 10 minutes and can be used once. Protect the file and keep it out of Git.');
          await refreshAdministration();
        } catch (error) {message(error.message, true); issue.disabled = false;}
      };
      row.append(issue); el('registrations').append(row);
    }
    if (!registrations.length) el('registrations').append(node('p', 'No clusters registered through onboarding yet. Existing configured agents remain available in the fleet view.'));
    for (const c of credentials) {
      const row = node('div', undefined, 'onboardingrow');
      const status = c.revokedAt ? 'Revoked' : c.usedAt ? 'Used' : new Date(c.expiresAt) <= new Date() ? 'Expired' : 'Active';
      row.append(node('strong', `${c.clusterID} · ${c.kind} · ${status}`), node('small', `Expires ${date(c.expiresAt)} · ${c.id}`));
      if (status === 'Active') {
        const revoke = node('button', 'Revoke');
        revoke.onclick = async () => {
          if (!window.confirm(`Revoke this ${c.kind} credential for ${c.clusterID}?`)) return;
          try {await post('/api/admin/revoke', {id: c.id}); message('Credential revoked. Stored history is retained.'); await refreshAdministration();}
          catch (error) {message(error.message, true);}
        };
        row.append(revoke);
      }
      el('credentials').append(row);
    }
    for (const event of events) el('adminaudit').append(node('p', `${date(event.createdAt)} · ${event.actor} · ${event.action} · ${event.target}`));
  }
  el('manageclusters').onclick = async () => {
    el('onboardingdialog').showModal(); message('Loading registrations…');
    try {await refreshAdministration(); message('Registration settings persist when you close this window.');}
    catch (error) {message(error.message, true);}
  };
  el('registrationform').onsubmit = async event => {
    event.preventDefault(); const data = Object.fromEntries(new FormData(event.target));
    data.namespaces = data.namespaces.split(',').map(x => x.trim()).filter(Boolean);
    try {await post('/api/admin/clusters', data); event.target.reset(); await refreshAdministration(); message('Cluster registered. Download an enrollment file to connect its agent.');}
    catch (error) {message(error.message, true);}
  };
})();
