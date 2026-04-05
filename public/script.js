let lists = {}, currentKey = '', monitorInterval = null, targetsList = [];
let checkedTargets = {}; // index -> boolean
let cachedMembers = []; // cached group members for compare
let lastQrUrl = ''; // Cegah flicker reload QR terus menerus

// ===== Init =====
fetchStatus(); fetchLists(); fetchTargets(); fetchSchedulesCount();
setInterval(fetchStatus, 3000);
setInterval(fetchSchedulesCount, 10000);

async function fetchSchedulesCount() {
    try {
        const r = await fetch('/api/schedules');
        const d = await r.json();
        const countSpan = document.getElementById('scheduleCount');
        if (countSpan) countSpan.textContent = d.length;
    } catch (e) { }
}

// ===== Status =====
let initialLoginSync = false;
async function fetchStatus() {
    try {
        const r = await fetch('/api/status'); const d = await r.json();
        
        // Sesuaikan toggle dengan state server saat pertama kali muat
        if (!initialLoginSync && typeof d.phoneLoginMode !== 'undefined') {
            switchLoginMode(d.phoneLoginMode ? 'phone' : 'qr');
            initialLoginSync = true;
        }

        const el = document.getElementById('statusText'), logoutBtn = document.getElementById('logoutBtn');
        const phoneArea = document.getElementById('phoneLoginArea');
        if (d.ready) {
            el.innerHTML = '<span style="color:green">✅ Ready</span>';
            document.getElementById('sendBtn').disabled = false;
            document.getElementById('qrArea').innerHTML = '';
            if (phoneArea) phoneArea.style.display = 'none';
            logoutBtn.style.display = 'inline-block';
            lastQrUrl = ''; // reset state
        } else {
            el.innerHTML = '<span style="color:red">❌ Belum Terkoneksi</span>';
            document.getElementById('sendBtn').disabled = true;
            logoutBtn.style.display = 'none';
            if (phoneArea) phoneArea.style.display = 'block';
            if (d.qr) {
                if (d.qr !== lastQrUrl) {
                    document.getElementById('qrArea').innerHTML = '<div class="qr-box"><h3 style="color:#25D366">📱 Scan QR Code</h3><img src="' + d.qr + '" style="display:block;margin:10px auto; max-width: 250px;"/><p style="color:#888;font-size:13px">Data QR diperbarui saat kadaluarsa.</p></div>';
                    lastQrUrl = d.qr;
                }
            } else {
                document.getElementById('qrArea').innerHTML = '<div class="qr-waiting"><h3 style="color:#d97706">⏳ Menunggu QR Code/Koneksi...</h3><p>Tunggu sebentar...</p></div>';
                lastQrUrl = '';
            }
        }
    } catch (e) { }
}

async function requestPairingCode() {
    const phone = document.getElementById('loginPhoneInput').value.trim();
    if (!phone) { toast('Masukkan nomor HP Anda', 'err'); return; }
    const btn = document.getElementById('btnPair');
    btn.disabled = true; btn.textContent = '⏳ Meminta...';
    try {
        const r = await fetch('/api/login/pair', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ phone }) });
        const d = await r.json();
        if (r.ok) {
            document.getElementById('pairingCodeResult').style.display = 'block';
            document.getElementById('pairingCodeText').textContent = d.code;
            toast('Kode Pairing didapat!', 'ok');
        } else toast(d.error || 'Gagal', 'err');
    } catch (e) { toast('Error: ' + e.message, 'err'); }
    btn.disabled = false; btn.textContent = 'Dapatkan Kode';
}

// ===== Targets with Checkboxes =====
async function fetchTargets() {
    const r = await fetch('/api/targets'); targetsList = await r.json();
    // Initialize all as checked if new
    targetsList.forEach((_, i) => { if (checkedTargets[i] === undefined) checkedTargets[i] = true; });
    renderTargets(); updateGroupSelect(); updateSendInfo();
}
function renderTargets() {
    const box = document.getElementById('targetChips');
    const selRow = document.getElementById('selectAllRow');
    if (targetsList.length === 0) { box.innerHTML = '<span class="no-targets">Belum ada target.</span>'; selRow.style.display = 'none'; return; }
    selRow.style.display = 'flex';
    box.innerHTML = '';
    targetsList.forEach((t, i) => {
        const isGroup = t.id.endsWith('@g.us');
        const isChecked = checkedTargets[i] !== false;
        const chip = document.createElement('span');
        chip.className = 'chip' + (isGroup ? ' chip-group' : '') + (isChecked ? '' : ' unchecked');
        chip.innerHTML = '<input type="checkbox" ' + (isChecked ? 'checked' : '') + ' onchange="toggleTarget(' + i + ', this.checked)">'
            + '<span class="chip-label">' + escHtml(t.label) + '</span>'
            + '<span class="chip-id">' + escHtml(t.id) + '</span>'
            + '<button class="chip-x" title="Hapus">✕</button>';
        chip.querySelector('.chip-x').onclick = () => removeTarget(i);
        box.appendChild(chip);
    });
    // Sync select all checkbox
    const allChecked = targetsList.every((_, i) => checkedTargets[i] !== false);
    document.getElementById('selectAllTargets').checked = allChecked;
}
function toggleTarget(index, val) {
    checkedTargets[index] = val;
    renderTargets(); updateSendInfo();
}
function toggleSelectAll() {
    const val = document.getElementById('selectAllTargets').checked;
    targetsList.forEach((_, i) => { checkedTargets[i] = val; });
    renderTargets(); updateSendInfo();
}
function updateSendInfo() {
    const selected = targetsList.filter((_, i) => checkedTargets[i] !== false);
    document.getElementById('sendInfo').textContent = selected.length > 0
        ? 'Akan dikirim ke ' + selected.length + ' target'
        : '⚠️ Tidak ada target yang dipilih';
}
function updateGroupSelect() {
    const sel = document.getElementById('groupSelect');
    const groups = targetsList.filter(t => t.id.endsWith('@g.us'));
    sel.innerHTML = '<option value="">-- Pilih Grup --</option>';
    groups.forEach(g => {
        const opt = document.createElement('option'); opt.value = g.id;
        opt.textContent = g.label + ' (' + g.id + ')'; sel.appendChild(opt);
    });
}
async function addTarget() {
    const id = document.getElementById('targetId').value.trim();
    const label = document.getElementById('targetLabel').value.trim();
    if (!id) { toast('ID tujuan wajib diisi', 'err'); return; }
    const r = await fetch('/api/targets', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id, label: label || id }) });
    if (r.ok) {
        document.getElementById('targetId').value = ''; document.getElementById('targetLabel').value = '';
        toast('Target ditambahkan!', 'ok'); fetchTargets();
    } else { const d = await r.json(); toast(d.error || 'Gagal', 'err'); }
}
async function removeTarget(index) {
    const r = await fetch('/api/targets/' + index, { method: 'DELETE' });
    if (r.ok) {
        delete checkedTargets[index];
        // Reindex checked
        const newChecked = {};
        targetsList.forEach((_, i) => { if (i < index) newChecked[i] = checkedTargets[i]; else if (i > index) newChecked[i - 1] = checkedTargets[i]; });
        checkedTargets = newChecked;
        toast('Target dihapus', 'ok'); fetchTargets();
    } else toast('Gagal menghapus', 'err');
}

// ===== Group Members =====
async function loadMembers() {
    const groupId = document.getElementById('groupSelect').value;
    if (!groupId) { toast('Pilih grup terlebih dahulu', 'err'); return; }
    const area = document.getElementById('membersArea'), info = document.getElementById('membersInfo');
    area.innerHTML = '<div class="members-loading">⏳ Mengambil data anggota...</div>'; info.textContent = '';
    try {
        const r = await fetch('/api/group-members/' + encodeURIComponent(groupId)); const d = await r.json();
        if (!r.ok) { area.innerHTML = '<p style="color:red">' + escHtml(d.error) + '</p>'; return; }
        cachedMembers = d.members;
        info.textContent = d.groupName + ' — ' + d.members.length + ' anggota';
        renderMembersTable(d.members); updateCompareVars();
    } catch (e) { area.innerHTML = '<p style="color:red">Gagal: ' + escHtml(e.message) + '</p>'; }
}
function renderMembersTable(members, extraCol) {
    let html = '<div class="members-table-wrap"><table><thead><tr><th>No</th><th>Nama</th><th>Nomor</th><th>Role</th>';
    if (extraCol) html += '<th>' + escHtml(extraCol) + '</th>';
    html += '</tr></thead><tbody>';
    members.forEach((m, i) => {
        let badge = '';
        if (m.isSuperAdmin) badge = '<span class="badge-superadmin">Owner</span>';
        else if (m.isAdmin) badge = '<span class="badge-admin">Admin</span>';
        html += '<tr><td>' + (i + 1) + '</td><td>' + escHtml(m.nama) + '</td><td>' + escHtml(m.nomor) + '</td><td>' + badge + '</td>';
        if (extraCol) html += '<td>' + (m._status || '') + '</td>';
        html += '</tr>';
    });
    html += '</tbody></table></div>';
    document.getElementById('membersArea').innerHTML = html;
}

// ===== Compare =====
function toggleCompare() {
    const panel = document.getElementById('comparePanel');
    panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
    if (panel.style.display === 'block') updateCompareVars();
}
function updateCompareVars() {
    const box = document.getElementById('cmpVarChecks');
    const colBox = document.getElementById('cmpColSelectors');
    box.innerHTML = ''; colBox.innerHTML = '';
    for (const key of Object.keys(lists)) {
        const id = 'cmpVar_' + key;
        box.innerHTML += '<label><input type="checkbox" id="' + id + '" value="' + escHtml(key) + '" onchange="updateColSelectors()"> ' + escHtml(key) + '</label>';
    }
}
function updateColSelectors() {
    const colBox = document.getElementById('cmpColSelectors');
    colBox.innerHTML = '';
    for (const key of Object.keys(lists)) {
        const cb = document.getElementById('cmpVar_' + key);
        if (!cb || !cb.checked) continue;
        const arr = lists[key];
        if (!arr || arr.length === 0) continue;
        const cols = Object.keys(arr[0]);
        let html = '<div class="compare-row"><label>Kolom ' + escHtml(key) + ':</label><select id="cmpCol_' + key + '">';
        cols.forEach(c => { html += '<option value="' + escHtml(c) + '"' + (c === 'nomor' ? ' selected' : '') + '>' + escHtml(c) + '</option>'; });
        html += '</select></div>';
        colBox.innerHTML += html;
    }
}
function runCompare() {
    if (cachedMembers.length === 0) { toast('Tampilkan anggota grup dulu', 'err'); return; }
    const groupCol = document.getElementById('cmpGroupCol').value;
    // Collect all values from selected variables
    const allVarValues = new Set();
    const varDetails = []; // { key, col, values }
    for (const key of Object.keys(lists)) {
        const cb = document.getElementById('cmpVar_' + key);
        if (!cb || !cb.checked) continue;
        const colSel = document.getElementById('cmpCol_' + key);
        if (!colSel) continue;
        const col = colSel.value;
        const vals = lists[key].map(row => normalizeNumber(row[col] || ''));
        vals.forEach(v => allVarValues.add(v));
        varDetails.push({ key, col, values: vals, raw: lists[key] });
    }
    if (varDetails.length === 0) { toast('Pilih minimal satu variabel', 'err'); return; }

    // Compare
    const found = [], notFound = [];
    cachedMembers.forEach(m => {
        const memberVal = normalizeNumber(m[groupCol] || '');
        if (allVarValues.has(memberVal)) {
            found.push(m);
        } else {
            notFound.push(m);
        }
    });

    // Also check: values in variables that are NOT in the group
    const memberVals = new Set(cachedMembers.map(m => normalizeNumber(m[groupCol] || '')));
    const notInGroup = [];
    varDetails.forEach(vd => {
        vd.raw.forEach((row, i) => {
            const val = normalizeNumber(row[vd.col] || '');
            if (!memberVals.has(val)) {
                notInGroup.push({ varName: vd.key, ...row, _val: val });
            }
        });
    });

    // Render results
    let html = '<div class="compare-results">';
    html += '<div style="margin-bottom:8px;">'
        + '<span class="compare-stat stat-green">✅ Ditemukan di grup: ' + found.length + '</span>'
        + '<span class="compare-stat stat-red">❌ Tidak di grup: ' + notFound.length + '</span>'
        + '</div>';

    html += '<h5>✅ Anggota yang ADA di variabel (' + found.length + ')</h5>';
    if (found.length > 0) {
        html += '<div class="members-table-wrap" style="max-height:200px;"><table><thead><tr><th>No</th><th>Nama</th><th>Nomor</th></tr></thead><tbody>';
        found.forEach((m, i) => { html += '<tr><td>' + (i + 1) + '</td><td>' + escHtml(m.nama) + '</td><td>' + escHtml(m.nomor) + '</td></tr>'; });
        html += '</tbody></table></div>';
    } else html += '<p class="hint">Tidak ada.</p>';

    html += '<h5>❌ Anggota grup yang TIDAK ADA di variabel (' + notFound.length + ')</h5>';
    if (notFound.length > 0) {
        html += '<div class="members-table-wrap" style="max-height:200px;"><table><thead><tr><th>No</th><th>Nama</th><th>Nomor</th></tr></thead><tbody>';
        notFound.forEach((m, i) => { html += '<tr><td>' + (i + 1) + '</td><td>' + escHtml(m.nama) + '</td><td>' + escHtml(m.nomor) + '</td></tr>'; });
        html += '</tbody></table></div>';
    } else html += '<p class="hint">Semua anggota ada di variabel.</p>';

    if (notInGroup.length > 0) {
        html += '<h5>⚠️ Data di variabel yang TIDAK ADA di grup (' + notInGroup.length + ')</h5>';
        html += '<div class="members-table-wrap" style="max-height:200px;"><table><thead><tr><th>No</th><th>Variabel</th>';
        const firstRow = notInGroup[0]; Object.keys(firstRow).filter(k => k !== 'varName' && k !== '_val').forEach(k => { html += '<th>' + escHtml(k) + '</th>'; });
        html += '</tr></thead><tbody>';
        notInGroup.forEach((row, i) => {
            html += '<tr><td>' + (i + 1) + '</td><td><span style="color:#8b5cf6;font-weight:600">' + escHtml(row.varName) + '</span></td>';
            Object.keys(row).filter(k => k !== 'varName' && k !== '_val').forEach(k => { html += '<td>' + escHtml(row[k]) + '</td>'; });
            html += '</tr>';
        });
        html += '</tbody></table></div>';
    }
    html += '</div>';
    document.getElementById('compareResults').innerHTML = html;
}
function normalizeNumber(val) {
    let s = String(val).replace(/\D/g, ''); // digits only
    if (s.startsWith('0')) s = '62' + s.substring(1);
    return s;
}

// ===== Lists / Cards =====
async function fetchLists() {
    const r = await fetch('/api/lists'); lists = await r.json(); renderCards();
    const r2 = await fetch('/api/default-message'); const d2 = await r2.json();
    document.getElementById('msgBox').value = d2.message;
}
function renderCards() {
    const grid = document.getElementById('cardsGrid'); grid.innerHTML = '';
    const varChecks = document.getElementById('privateVarCheckboxes');
    varChecks.innerHTML = '';

    for (const key of Object.keys(lists)) {
        const card = document.createElement('div'); card.className = 'card';
        card.innerHTML = '<div class="card-title">' + escHtml(key) + '</div><div class="card-count">' + lists[key].length + ' data</div>';
        card.onclick = () => viewList(key); grid.appendChild(card);

        // Populate private variable select
        const lbl = document.createElement('label');
        lbl.style.display = 'flex'; lbl.style.alignItems = 'center'; lbl.style.gap = '6px'; lbl.style.cursor = 'pointer';
        lbl.innerHTML = `<input type="checkbox" class="priv-var-check" value="${escHtml(key)}" style="accent-color:#8b5cf6;"> ${escHtml(key)} (${lists[key].length} data)`;
        varChecks.appendChild(lbl);
    }
    const addCard = document.createElement('div'); addCard.className = 'add-card';
    addCard.textContent = '＋ Tambah Variabel'; addCard.onclick = openAdd; grid.appendChild(addCard);
}

// ===== View Modal =====
function viewList(key) {
    currentKey = key; document.getElementById('viewTitle').textContent = key;
    const arr = lists[key];
    if (!arr || arr.length === 0) { document.getElementById('viewBody').innerHTML = '<p>Tidak ada data.</p>'; }
    else {
        const headers = Object.keys(arr[0]);
        let t = '<table><thead><tr><th>No</th>'; headers.forEach(h => t += '<th>' + escHtml(h) + '</th>');
        t += '</tr></thead><tbody>';
        arr.forEach((row, i) => { t += '<tr><td>' + (i + 1) + '</td>'; headers.forEach(h => t += '<td>' + escHtml(row[h] || '') + '</td>'); t += '</tr>'; });
        t += '</tbody></table>'; document.getElementById('viewBody').innerHTML = t;
    }
    const codeEl = document.getElementById('viewCode'); codeEl.textContent = generateJS(key, arr); codeEl.style.display = 'block';
    document.getElementById('viewOverlay').classList.add('show');
}
function closeView() { document.getElementById('viewOverlay').classList.remove('show'); }
async function delList() {
    if (!confirm('Yakin hapus "' + currentKey + '"?')) return;
    const r = await fetch('/api/lists/' + encodeURIComponent(currentKey), { method: 'DELETE' });
    if (r.ok) { toast(currentKey + ' dihapus!', 'ok'); closeView(); fetchLists(); } else toast('Gagal', 'err');
}

// ===== Edit Modal =====
function openEdit() {
    closeView(); const arr = lists[currentKey] || [];
    document.getElementById('editName').value = currentKey;
    document.getElementById('editTitle').textContent = '✏️ Edit: ' + currentKey;
    if (arr.length > 0) {
        const h = Object.keys(arr[0]); let csv = h.join(',') + '\n';
        arr.forEach(row => { csv += h.map(k => row[k] || '').join(',') + '\n'; });
        document.getElementById('editCSV').value = csv.trim();
    } else document.getElementById('editCSV').value = '';
    document.getElementById('editPreview').style.display = 'none';
    document.getElementById('editOverlay').classList.add('show');
}
function closeEdit() { document.getElementById('editOverlay').classList.remove('show'); }
document.getElementById('editCSV').addEventListener('input', function () {
    const n = document.getElementById('editName').value.trim(), parsed = parseCSV(this.value), prev = document.getElementById('editPreview');
    if (parsed.length > 0) { prev.textContent = generateJS(n, parsed); prev.style.display = 'block'; } else prev.style.display = 'none';
});
async function saveEdit() {
    const n = document.getElementById('editName').value.trim(), csv = document.getElementById('editCSV').value.trim();
    if (!csv) { toast('Data CSV kosong', 'err'); return; }
    const r = await fetch('/api/lists', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: n, csv }) });
    if (r.ok) { toast(n + ' diperbarui!', 'ok'); closeEdit(); fetchLists(); } else { const d = await r.json(); toast(d.error || 'Gagal', 'err'); }
}

// ===== Add Modal =====
function openAdd() {
    document.getElementById('addName').value = ''; document.getElementById('addCSV').value = '';
    document.getElementById('addPreview').style.display = 'none'; document.getElementById('addOverlay').classList.add('show');
}
function closeAdd() { document.getElementById('addOverlay').classList.remove('show'); }
document.getElementById('addCSV').addEventListener('input', function () {
    const n = document.getElementById('addName').value.trim() || 'namaVariabel', parsed = parseCSV(this.value), prev = document.getElementById('addPreview');
    if (parsed.length > 0) { prev.textContent = generateJS(n, parsed); prev.style.display = 'block'; } else prev.style.display = 'none';
});
document.getElementById('addName').addEventListener('input', function () { document.getElementById('addCSV').dispatchEvent(new Event('input')); });
async function saveList() {
    const n = document.getElementById('addName').value.trim(), csv = document.getElementById('addCSV').value.trim();
    if (!n) { toast('Nama wajib diisi', 'err'); return; }
    if (!csv) { toast('CSV wajib diisi', 'err'); return; }
    const r = await fetch('/api/lists', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: n, csv }) });
    if (r.ok) { toast(n + ' tersimpan!', 'ok'); closeAdd(); fetchLists(); } else { const d = await r.json(); toast(d.error || 'Gagal', 'err'); }
}

// ===== Monitor =====
async function toggleMonitor() {
    const r = await fetch('/api/monitor/toggle', { method: 'POST' }); const d = await r.json();
    document.getElementById('monitorLabel').textContent = d.on ? 'ON' : 'OFF';
    document.getElementById('monitorToggle').checked = d.on;
    if (d.on) { pollMonitor(); monitorInterval = setInterval(pollMonitor, 2000); }
    else { clearInterval(monitorInterval); monitorInterval = null; document.getElementById('monitorBox').innerHTML = '<div class="monitor-empty">Monitor mati.</div>'; }
}
async function pollMonitor() {
    try {
        const r = await fetch('/api/monitor'); const d = await r.json();
        const box = document.getElementById('monitorBox'); if (!d.on) return;
        if (d.messages.length === 0) { box.innerHTML = '<div class="monitor-empty" style="color:#666">Menunggu pesan masuk...</div>'; return; }
        let html = '';
        d.messages.forEach(m => {
            const tag = m.isGroup ? '<span class="msg-group-tag">GRUP</span>' : '<span class="msg-private-tag">PRIVATE</span>';
            html += '<div class="msg-entry"><span class="msg-time">[' + escHtml(m.time) + ']</span> ' + tag
                + ' <span class="msg-from">' + escHtml(m.from) + '</span><br><span class="msg-body">' + escHtml(m.body) + '</span></div>';
        });
        box.innerHTML = html;
    } catch (e) { }
}

// ===== Logout =====
async function doLogout() {
    if (!confirm('Yakin logout?')) return;
    const btn = document.getElementById('logoutBtn'); btn.disabled = true; btn.textContent = '⏳...';
    try {
        const r = await fetch('/api/logout', { method: 'POST' }); const d = await r.json();
        if (r.ok) toast('Berhasil logout!', 'ok'); else toast(d.error || 'Gagal', 'err');
    } catch (e) { toast('Error: ' + e.message, 'err'); }
    btn.disabled = false; btn.textContent = '🚪 Logout';
}

// ===== Login Mode Toggle =====
async function switchLoginMode(mode) {
    const qrPanel = document.getElementById('loginPanelQr');
    const phonePanel = document.getElementById('loginPanelPhone');
    const qrBtn = document.getElementById('toggleQrBtn');
    const phoneBtn = document.getElementById('togglePhoneBtn');
    const qrArea = document.getElementById('qrArea');

    try {
        await fetch('/api/login/mode', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mode })
        });
    } catch (e) {
        console.error('Gagal update mode:', e);
    }

    if (mode === 'qr') {
        qrPanel.style.display = 'block';
        phonePanel.style.display = 'none';
        qrArea.style.display = 'block'; // Tampilkan kembali QR
        qrBtn.style.background = '#008b5e';
        qrBtn.style.color = 'white';
        phoneBtn.style.background = '#f1f5f9';
        phoneBtn.style.color = '#555';
    } else {
        qrPanel.style.display = 'none';
        phonePanel.style.display = 'block';
        qrArea.style.display = 'none'; // Sembunyikan QR agar tidak konflik
        phoneBtn.style.background = '#0ea5e9';
        phoneBtn.style.color = 'white';
        qrBtn.style.background = '#f1f5f9';
        qrBtn.style.color = '#555';
        // Reset pairing result
        document.getElementById('pairingCodeResult').style.display = 'none';
    }
}


// ===== Mode Toggle & Private Sending State =====
let isPrivateSending = false;
let cancelPrivateSend = false;

function toggleSendMode() {
    const isPrivate = document.querySelector('input[name="sendMode"][value="pribadi"]').checked;
    document.getElementById('sectionTargetGrup').style.display = isPrivate ? 'none' : 'block';
    document.getElementById('privateVarSelectArea').style.display = isPrivate ? 'block' : 'none';
    if (!isPrivate) {
        document.getElementById('privateProgressArea').style.display = 'none';
        document.getElementById('stopBtn').style.display = 'none';
    }
}

function stopPrivateSend() {
    if (isPrivateSending) {
        cancelPrivateSend = true;
        document.getElementById('stopBtn').disabled = true;
        document.getElementById('stopBtn').textContent = 'Menghentikan...';
    }
}

// ===== Send Message (Grup & Pribadi) =====
async function sendMsg() {
    const isPrivate = document.querySelector('input[name="sendMode"][value="pribadi"]').checked;
    if (isPrivate) {
        await startPrivateSend();
    } else {
        await sendGroupMsg();
    }
}

async function sendGroupMsg() {
    const btn = document.getElementById('sendBtn');
    let msg = document.getElementById('msgBox').value;
    if (document.getElementById('notisCheck').checked) {
        msg += '\n\n```This number does not respond to messages. Please contact Anugrah (62 822 7743 1128) or another admin instead.```';
    }
    const selectedIndices = [];
    targetsList.forEach((_, i) => { if (checkedTargets[i] !== false) selectedIndices.push(i); });
    if (selectedIndices.length === 0) { toast('Tidak ada target yang dicentang', 'err'); return; }
    btn.disabled = true; btn.textContent = '⏳ Mengirim...';
    try {
        const hideTag = document.getElementById('hideTagCheck').checked;
        const r = await fetch('/api/send', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message: msg, selectedTargets: selectedIndices, hideTag: hideTag }) });
        const d = await r.json();
        if (r.ok) toast('🎉 Terkirim ke ' + d.sent + '/' + d.total + ' target!', 'ok');
        else toast(d.error || 'Gagal', 'err');
    } catch (e) { toast('Error: ' + e.message, 'err'); }
    btn.disabled = false; btn.textContent = '🚀 Kirim Pesan';
}

async function startPrivateSend() {
    // Collect selected variables
    const checkNodes = document.querySelectorAll('.priv-var-check:checked');
    if (checkNodes.length === 0) { toast('Centang minimal satu variabel tujuan!', 'err'); return; }

    // Combine all selected targets
    let targetList = [];
    checkNodes.forEach(node => {
        const varName = node.value;
        if (lists[varName]) {
            targetList = targetList.concat(lists[varName]);
        }
    });

    if (targetList.length === 0) { toast('Variabel tujuan kosong!', 'err'); return; }

    const numberColumn = Object.keys(targetList[0]).find(k => ['nomor', 'notelp', 'no', 'phone', 'hp'].includes(k.toLowerCase())) || Object.keys(targetList[0])[0];

    let baseMsg = document.getElementById('msgBox').value;
    if (document.getElementById('notisCheck').checked) {
        baseMsg += '\n\n```This number does not respond to messages. Please contact Anugrah (62 822 7743 1128) or another admin instead.```';
    }

    // UI Setup
    isPrivateSending = true;
    cancelPrivateSend = false;
    document.getElementById('sendBtn').style.display = 'none';
    const stopBtn = document.getElementById('stopBtn');
    stopBtn.style.display = 'inline-block';
    stopBtn.disabled = false;
    stopBtn.textContent = '🛑 Berhenti';

    const progArea = document.getElementById('privateProgressArea');
    const progText = document.getElementById('privateProgressText');
    const progTime = document.getElementById('privateProgressTime');
    const progBar = document.getElementById('privateProgressBar');
    progArea.style.display = 'block';

    let sentCount = 0;
    const total = targetList.length;

    for (let i = 0; i < total; i++) {
        if (cancelPrivateSend) {
            toast('Pengiriman dihentikan pengguna.', 'err');
            break;
        }

        const person = targetList[i];
        const targetNumber = person[numberColumn];
        if (!targetNumber) continue;

        // Personalize Message using ++kolom
        let personalMsg = baseMsg;
        Object.keys(person).forEach(k => {
            // Replace ++nama with the person's 'nama' data
            // Also supports ++nomor, ++alamat, etc., depending on the CSV headers
            const regex = new RegExp(`\\+\\+${k}\\b`, 'g');
            personalMsg = personalMsg.replace(regex, person[k]);
        });

        // Update UI
        progText.textContent = `Mengirim: ${i + 1} / ${total}`;
        progTime.textContent = 'Mengirim pesan...';
        progBar.style.width = `${((i) / total) * 100}%`;

        try {
            const r = await fetch('/api/send-private', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ target: targetNumber, message: personalMsg })
            });
            if (r.ok) sentCount++;
        } catch (e) {
            console.error('Send Error:', e);
        }

        progBar.style.width = `${((i + 1) / total) * 100}%`;

        // Delay if not last
        if (i < total - 1 && !cancelPrivateSend) {
            const delayMs = Math.floor(Math.random() * (62000 - 7000 + 1)) + 7000;
            let timeLeft = Math.ceil(delayMs / 1000);

            for (let s = timeLeft; s > 0; s--) {
                if (cancelPrivateSend) break;
                progTime.textContent = `Jeda: ${s} detik...`;
                await new Promise(resolve => setTimeout(resolve, 1000));
            }
        }
    }

    // Wrap up
    isPrivateSending = false;
    document.getElementById('sendBtn').style.display = 'inline-block';
    stopBtn.style.display = 'none';
    progTime.textContent = cancelPrivateSend ? 'Dihentikan.' : 'Selesai!';
    toast(`Pengiriman selesai. ${sentCount} terkirim.`, 'ok');
}

// ===== Scheduled Messages =====
function openScheduleModal() {
    const isPrivate = document.querySelector('input[name="sendMode"][value="pribadi"]').checked;
    if (isPrivate) {
        const checkNodes = document.querySelectorAll('.priv-var-check:checked');
        if (checkNodes.length === 0) { toast('Centang minimal satu variabel tujuan!', 'err'); return; }
    } else {
        const selectedIndices = [];
        targetsList.forEach((_, i) => { if (checkedTargets[i] !== false) selectedIndices.push(i); });
        if (selectedIndices.length === 0) { toast('Tidak ada target yang dicentang', 'err'); return; }
    }
    document.getElementById('scheduleDateTime').value = '';
    document.getElementById('scheduleOverlay').classList.add('show');
}
function closeScheduleModal() { document.getElementById('scheduleOverlay').classList.remove('show'); }

function toggleScheduleType() {
    const isRecurring = document.querySelector('input[name="scheduleType"][value="recurring"]').checked;
    document.getElementById('scheduleOnceArea').style.display = isRecurring ? 'none' : 'block';
    document.getElementById('scheduleRecurringArea').style.display = isRecurring ? 'block' : 'none';
}

async function submitSchedule() {
    const isRecurring = document.querySelector('input[name="scheduleType"][value="recurring"]').checked;
    const scheduleTypeVal = isRecurring ? 'recurring' : 'once';
    
    let timeToProcess = 0, cronDays = [], cronTime = '';
    
    if (!isRecurring) {
        const timeVal = document.getElementById('scheduleDateTime').value;
        if (!timeVal) { toast('Pilih waktu pengiriman!', 'err'); return; }
        timeToProcess = new Date(timeVal).getTime();
        if (timeToProcess <= Date.now()) { toast('Waktu jadwal harus lebih dari sekarang!', 'err'); return; }
    } else {
        const checkboxes = document.querySelectorAll('.day-check:checked');
        checkboxes.forEach(cb => cronDays.push(cb.value));
        if (cronDays.length === 0) { toast('Pilih minimal satu hari!', 'err'); return; }
        cronTime = document.getElementById('scheduleCronTime').value;
        if (!cronTime) { toast('Pilih jam pengiriman!', 'err'); return; }
    }

    const isPrivate = document.querySelector('input[name="sendMode"][value="pribadi"]').checked;
    let baseMsg = document.getElementById('msgBox').value;
    if (document.getElementById('notisCheck').checked) {
        baseMsg += '\n\n```This number does not respond to messages. Please contact Anugrah (62 822 7743 1128) or another admin instead.```';
    }

    let payload = null;
    let type = '';

    if (isPrivate) {
        type = 'pribadi';
        const checkNodes = document.querySelectorAll('.priv-var-check:checked');
        let targetList = [];
        checkNodes.forEach(node => {
            const varName = node.value;
            if (lists[varName]) targetList = targetList.concat(lists[varName]);
        });
        const numberColumn = Object.keys(targetList[0]).find(k => ['nomor', 'notelp', 'no', 'phone', 'hp'].includes(k.toLowerCase())) || Object.keys(targetList[0])[0];

        const privatePayloads = [];
        for (let i = 0; i < targetList.length; i++) {
            const person = targetList[i];
            const targetNumber = person[numberColumn];
            if (!targetNumber) continue;
            let personalMsg = baseMsg;
            Object.keys(person).forEach(k => {
                personalMsg = personalMsg.replace(new RegExp(`\\+\\+${k}\\b`, 'g'), person[k]);
            });
            privatePayloads.push({ target: targetNumber, message: personalMsg });
        }
        payload = privatePayloads;
    } else {
        type = 'grup';
        const selectedIndices = [];
        targetsList.forEach((_, i) => { if (checkedTargets[i] !== false) selectedIndices.push(i); });
        payload = {
            message: baseMsg,
            selectedTargets: selectedIndices,
            hideTag: document.getElementById('hideTagCheck').checked
        };
    }

    const btn = document.querySelector('#scheduleOverlay .btn-save');
    btn.disabled = true; btn.textContent = '⏳ Menyimpan...';
    try {
        const r = await fetch('/api/schedules', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ 
                time: timeToProcess, 
                payload, 
                type, 
                scheduleType: scheduleTypeVal, 
                cronDays, 
                cronTime 
            })
        });
        const d = await r.json();
        if (r.ok) {
            toast('Jadwal berhasil disimpan!', 'ok');
            closeScheduleModal();
            fetchSchedulesCount();
        } else {
            toast(d.error || 'Gagal menyimpan jadwal', 'err');
        }
    } catch (e) { toast('Error: ' + e.message, 'err'); }
    btn.disabled = false; btn.textContent = '🕒 Buat Jadwal';
}

async function openSchedulesList() {
    document.getElementById('schedulesListOverlay').classList.add('show');
    const body = document.getElementById('schedulesListBody');
    body.innerHTML = '<p>Loading...</p>';
    try {
        const r = await fetch('/api/schedules');
        const d = await r.json();
        fetchSchedulesCount();
        if (!Array.isArray(d) || d.length === 0) {
            body.innerHTML = '<p class="hint">Tidak ada pesan terjadwal.</p>';
            return;
        }
        let html = '<div class="members-table-wrap"><table><thead><tr><th>Dibuat</th><th>Jadwal</th><th>Tipe</th><th>Detail</th><th>Aksi</th></tr></thead><tbody>';
        d.forEach(s => { // Sort won't accurately reflect both array types properly here for now, so removed sort to keep code neat
            let dateStr = '';
            if (s.scheduleType === 'recurring') {
                const dayMap = {'1':'Sen','2':'Sel','3':'Rab','4':'Kam','5':'Jum','6':'Sab','7':'Min'};
                const dNames = s.cronDays.map(x => dayMap[x]).join(', ');
                dateStr = `<span style="color:#f59e0b;font-weight:600;">🔄 Rutin: Tiap ${dNames} (${s.cronTime})</span>`;
            } else {
                dateStr = `<span style="font-weight:600;color:#25d366">${new Date(s.timeToProcess).toLocaleString('id-ID')}</span>`;
            }
            
            let info = s.type === 'grup' ? `${s.payload.selectedTargets.length} grup/target` : `${s.payload.length} orang(japri)`;
            let msgPreview = s.type === 'grup' ? s.payload.message : s.payload[0].message;
            if (msgPreview.length > 30) msgPreview = msgPreview.substring(0, 30) + '...';
            html += `<tr>
                <td style="font-size:12px;color:#666;">${s.createdAt || '-'}</td>
                <td>${dateStr}</td>
                <td><span class="badge-${s.type === 'grup' ? 'admin' : 'superadmin'}">${s.type}</span></td>
                <td style="font-size:12px;">Ke: ${info}<br>Pesan: ${escHtml(msgPreview)}</td>
                <td style="display:flex;gap:6px;">
                    <button class="btn btn-blue btn-sm" onclick="openEditSchedule('${s.id}')">✏️ Edit</button>
                    <button class="btn btn-red btn-sm" onclick="deleteSchedule('${s.id}')">🗑 Hapus</button>
                </td>
            </tr>`;
        });
        html += '</tbody></table></div>';
        body.innerHTML = html;
    } catch (e) {
        body.innerHTML = '<p style="color:red">Error memuat data jadwal.</p>';
    }
}
function closeSchedulesList() { document.getElementById('schedulesListOverlay').classList.remove('show'); }

async function deleteSchedule(id) {
    if (!confirm('Yakin membatalkan pesan jadwal ini?')) return;
    try {
        const r = await fetch('/api/schedules/' + id, { method: 'DELETE' });
        if (r.ok) {
            toast('Jadwal dibatalkan', 'ok');
            openSchedulesList();
        } else toast('Gagal membatalkan jadwal', 'err');
    } catch (e) { toast('Error: ' + e.message, 'err'); }
}

let editingScheduleId = null;
async function openEditSchedule(id) {
    // Fetch latest schedules
    const r = await fetch('/api/schedules');
    const all = await r.json();
    const s = all.find(x => x.id === id);
    if (!s) { toast('Jadwal tidak ditemukan', 'err'); return; }
    editingScheduleId = id;

    // Populate message
    let msg = s.type === 'grup' ? s.payload.message : (s.payload[0] ? s.payload[0].message : '');
    document.getElementById('editSchedMsg').value = msg;

    // Populate schedule type
    const isRecurring = s.scheduleType === 'recurring';
    document.querySelector(`input[name="editSchedType"][value="${s.scheduleType}"]`).checked = true;
    document.getElementById('editSchedOnceArea').style.display = isRecurring ? 'none' : 'block';
    document.getElementById('editSchedRecurringArea').style.display = isRecurring ? 'block' : 'none';

    if (!isRecurring && s.timeToProcess) {
        // Format for datetime-local
        const dt = new Date(s.timeToProcess);
        const pad = n => String(n).padStart(2, '0');
        document.getElementById('editSchedDateTime').value = `${dt.getFullYear()}-${pad(dt.getMonth()+1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`;
    }
    if (isRecurring) {
        document.getElementById('editSchedCronTime').value = s.cronTime || '';
        document.querySelectorAll('.edit-day-check').forEach(cb => {
            cb.checked = s.cronDays && s.cronDays.includes(cb.value);
        });
    }

    document.getElementById('editScheduleOverlay').classList.add('show');
}
function closeEditSchedule() { document.getElementById('editScheduleOverlay').classList.remove('show'); editingScheduleId = null; }
function toggleEditScheduleType() {
    const isRecurring = document.querySelector('input[name="editSchedType"][value="recurring"]').checked;
    document.getElementById('editSchedOnceArea').style.display = isRecurring ? 'none' : 'block';
    document.getElementById('editSchedRecurringArea').style.display = isRecurring ? 'block' : 'none';
}
async function saveEditSchedule() {
    if (!editingScheduleId) return;
    const message = document.getElementById('editSchedMsg').value;
    const isRecurring = document.querySelector('input[name="editSchedType"][value="recurring"]').checked;
    const scheduleType = isRecurring ? 'recurring' : 'once';
    let body = { message, scheduleType };
    if (!isRecurring) {
        const timeVal = document.getElementById('editSchedDateTime').value;
        if (!timeVal) { toast('Pilih waktu!', 'err'); return; }
        body.time = new Date(timeVal).getTime();
    } else {
        const cronDays = [];
        document.querySelectorAll('.edit-day-check:checked').forEach(cb => cronDays.push(cb.value));
        if (cronDays.length === 0) { toast('Pilih minimal satu hari!', 'err'); return; }
        const cronTime = document.getElementById('editSchedCronTime').value;
        if (!cronTime) { toast('Pilih jam!', 'err'); return; }
        body.cronDays = cronDays;
        body.cronTime = cronTime;
    }
    const btn = document.getElementById('editSchedSaveBtn');
    btn.disabled = true; btn.textContent = '⏳ Menyimpan...';
    try {
        const r = await fetch('/api/schedules/' + editingScheduleId, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (r.ok) {
            toast('✅ Jadwal berhasil diperbarui!', 'ok');
            closeEditSchedule();
            openSchedulesList();
        } else { const d = await r.json(); toast(d.error || 'Gagal', 'err'); }
    } catch (e) { toast('Error: ' + e.message, 'err'); }
    btn.disabled = false; btn.textContent = '💾 Simpan Perubahan';
}

// ===== Helpers =====
function parseCSV(csv) {
    const lines = csv.trim().split('\n'); if (lines.length < 2) return [];
    const headers = lines[0].split(',').map(s => s.trim()); const result = [];
    for (let i = 1; i < lines.length; i++) {
        const cols = lines[i].split(',').map(s => s.trim()); if (cols.length < headers.length) continue;
        const obj = {}; headers.forEach((h, idx) => obj[h] = cols[idx]); result.push(obj);
    }
    return result;
}
function generateJS(name, data) {
    let js = 'const ' + name + ' = [\n';
    data.forEach((row, i) => {
        js += '    { '; const entries = Object.entries(row);
        entries.forEach(([k, v], j) => { js += k + ": '" + v + "'"; if (j < entries.length - 1) js += ', '; });
        js += ' }'; if (i < data.length - 1) js += ','; js += '\n';
    });
    js += '];'; return js;
}
function escHtml(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }
function toast(msg, type) {
    const el = document.createElement('div'); el.className = 'toast toast-' + type; el.textContent = msg;
    document.body.appendChild(el); setTimeout(() => el.remove(), 3000);
}

// Fitur Auto Update Server
async function triggerSystemUpdate() {
    const isConfirm = confirm("⚠️ PERINGATAN: Anda akan menarik kode terbaru dari Github dan me-restart mesin server Google Cloud.\n\nWebsite akan mati sementara sekitar 5-10 detik. Lanjutkan?");
    if (!isConfirm) return;

    toast('🔄 Mengirim perintah update...', 'info');
    
    try {
        const res = await fetch('/api/system-update', { method: 'POST' });
        
        // Peringatan bahwa website mati sementara
        toast('⏳ Server sedang me-restart, halaman memuat ulang...', 'success');
        
        // Setelah 8 detik, paksa reload halaman untuk melihat hasil barunya
        setTimeout(() => {
            window.location.reload(true);
        }, 8000);
        
    } catch (err) {
        toast('❌ Gagal menghubungi server saat update', 'error');
    }
}
