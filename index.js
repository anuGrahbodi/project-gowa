const { Client, LocalAuth } = require('whatsapp-web.js');
const qrcode = require('qrcode-terminal');
const QRCode = require('qrcode');
const express = require('express');
const fs = require('fs');
const path = require('path');

const app = express();
const port = 3000;

app.use(express.urlencoded({ extended: true }));
app.use(express.json());
app.use(express.static(path.join(__dirname, 'public')));

// ===== Data Persistence =====
const DATA_FILE = path.join(__dirname, 'lists.json');
const TARGETS_FILE = path.join(__dirname, 'targets.json');
const SCHEDULES_FILE = path.join(__dirname, 'schedules.json');

function loadLists() {
    if (fs.existsSync(DATA_FILE)) {
        return JSON.parse(fs.readFileSync(DATA_FILE, 'utf-8'));
    }
    const defaultData = {
        listMedanKota: [
            { nama: 'bg ghani', nomor: '628170122004' }
        ],
        listMedanUtara: [
            { nama: 'robi', nomor: '6289524210854' },
            { nama: 'bg abdi', nomor: '6282363533971' }
        ]
    };
    saveLists(defaultData);
    return defaultData;
}

function saveLists(data) {
    fs.writeFileSync(DATA_FILE, JSON.stringify(data, null, 2));
}

function loadTargets() {
    if (fs.existsSync(TARGETS_FILE)) {
        return JSON.parse(fs.readFileSync(TARGETS_FILE, 'utf-8'));
    }
    const defaultTargets = [
        { id: '120363307560468751@g.us', label: 'Grup Default' }
    ];
    saveTargets(defaultTargets);
    return defaultTargets;
}

function saveTargets(data) {
    fs.writeFileSync(TARGETS_FILE, JSON.stringify(data, null, 2));
}

function loadSchedules() {
    if (fs.existsSync(SCHEDULES_FILE)) {
        try { return JSON.parse(fs.readFileSync(SCHEDULES_FILE, 'utf-8')); } catch (e) { return []; }
    }
    const defaultData = [];
    saveSchedules(defaultData);
    return defaultData;
}

function saveSchedules(data) {
    fs.writeFileSync(SCHEDULES_FILE, JSON.stringify(data, null, 2));
}

let lists = loadLists();
let targets = loadTargets();
let schedules = loadSchedules();

// ===== WhatsApp Client =====
let client = null;
let isReady = false;
let latestQrDataUrl = null;
let monitorOn = false;
let incomingMessages = []; // buffer pesan masuk (max 100)

function createClient() {
    client = new Client({
        authStrategy: new LocalAuth(),
        puppeteer: {
            // Ditambahkan optimasi untuk jalan di server (GCP/Linux tanpa GUI)
            headless: true,
            args: [
                '--no-sandbox',
                '--disable-setuid-sandbox',
                '--disable-dev-shm-usage',
                '--disable-accelerated-2d-canvas',
                '--no-first-run',
                '--no-zygote',
                '--disable-gpu'
            ]
        }
    });

    client.on('qr', async (qr) => {
        qrcode.generate(qr, { small: true });
        try {
            latestQrDataUrl = await QRCode.toDataURL(qr, { width: 300 });
            console.log('📱 QR Code baru tersedia di halaman web.');
        } catch (err) {
            console.error('Gagal generate QR image:', err);
        }
    });

    client.on('authenticated', () => {
        console.log('🔐 Authenticated! Memuat data session...');
    });

    client.on('auth_failure', (msg) => {
        console.error('❌ Authentication failure:', msg);
        latestQrDataUrl = null;
        isReady = false;
    });

    client.on('ready', () => {
        console.log('✅ WhatsApp Web Client is ready!');
        isReady = true;
        latestQrDataUrl = null;
    });

    client.on('disconnected', (reason) => {
        console.log('🔌 Client disconnected:', reason);
        isReady = false;
        latestQrDataUrl = null;
    });

    // Monitor pesan masuk
    client.on('message', (msg) => {
        if (!monitorOn) return;
        const entry = {
            time: new Date().toLocaleTimeString('id-ID'),
            from: msg.from,
            body: msg.body ? msg.body.substring(0, 200) : '(media/kosong)',
            isGroup: msg.from.endsWith('@g.us')
        };
        incomingMessages.unshift(entry);
        if (incomingMessages.length > 100) incomingMessages.pop();
        console.log(`📩 [Monitor] Pesan dari ${msg.from}: ${entry.body.substring(0, 50)}`);
    });

    client.initialize().catch(err => {
        console.error('⚠️ WhatsApp client gagal initialize:', err.message);
        console.log('🔄 Server tetap berjalan. Silakan tutup browser WA lama dan restart server.');
    });
}

// groupId sekarang diambil dari targets (dinamis)

// ===== Helper Functions =====
// Deteksi kolom nomor secara dinamis
function getPhoneKey(obj) {
    const keys = Object.keys(obj);
    // Cari kolom yang kemungkinan berisi nomor telepon
    const phoneKeys = ['nomor', 'notelp', 'no', 'phone', 'hp', 'telepon', 'nohp', 'nomer'];
    for (const pk of phoneKeys) {
        if (keys.includes(pk)) return pk;
    }
    // Fallback: kolom kedua (kolom pertama biasanya nama)
    return keys.length > 1 ? keys[1] : keys[0];
}

const processList = (list) => {
    if (list.length === 0) return '';
    const phoneKey = getPhoneKey(list[0]);
    const nameKey = Object.keys(list[0]).find(k => k !== phoneKey) || phoneKey;
    let teks = '';
    for (let orang of list) {
        teks += `${orang[nameKey]}, @${orang[phoneKey]}\n`;
    }
    return teks;
};

const getMentionsList = (list) => {
    if (list.length === 0) return [];
    const phoneKey = getPhoneKey(list[0]);
    const mentions = [];
    for (let orang of list) {
        mentions.push(`${orang[phoneKey]}@c.us`);
    }
    return mentions;
};

function buildDefaultMessage() {
    let msg = 'tes:\n';
    const keys = Object.keys(lists);
    keys.forEach((key, i) => {
        msg += `\n*List ${i + 1}:*\n\${${key}}\n`;
    });
    msg += 'hi tes';
    return msg;
}

// ===== Schedules Processor =====
async function processSchedule(job) {
    console.log(`⏰ Menjalankan pesan berjadwal [${job.id}] tipe ${job.type}...`);
    try {
        if (job.type === 'grup') {
            const p = job.payload;
            let sendTargets = [];
            if (Array.isArray(p.selectedTargets) && p.selectedTargets.length > 0) {
                sendTargets = p.selectedTargets.map(i => targets[i]).filter(Boolean);
            } else {
                sendTargets = targets;
            }
            if (sendTargets.length === 0) return;

            let userMessage = p.message || '';
            let finalMessage = userMessage;
            const mentionIds = [];

            for (const [key, list] of Object.entries(lists)) {
                const regex = new RegExp(`\\$\\{${key}\\}`, 'g');
                if (regex.test(userMessage)) {
                    finalMessage = finalMessage.replace(new RegExp(`\\$\\{${key}\\}`, 'g'), processList(list));
                    mentionIds.push(...getMentionsList(list));
                }
            }

            if (p.hideTag) {
                for (const target of sendTargets) {
                    if (target.id.endsWith('@g.us')) {
                        try {
                            const chat = await client.getChatById(target.id);
                            const participants = chat.participants || [];
                            for (const part of participants) {
                                mentionIds.push(part.id._serialized);
                            }
                        } catch (e) { }
                    }
                }
            }

            const uniqueIds = [...new Set(mentionIds)];
            const mentionContacts = [];
            for (const cid of uniqueIds) {
                try {
                    const contact = await client.getContactById(cid);
                    mentionContacts.push(contact);
                } catch (e) { }
            }

            for (const target of sendTargets) {
                try {
                    if (mentionContacts.length > 0) {
                        await client.sendMessage(target.id, finalMessage, { mentions: mentionContacts });
                    } else {
                        await client.sendMessage(target.id, finalMessage);
                    }
                } catch (e) { }
            }
        }
        else if (job.type === 'pribadi') {
            const arr = job.payload; // array of { target, message }
            for (let i = 0; i < arr.length; i++) {
                const item = arr[i];
                let chatId = String(item.target).replace(/\D/g, '');
                if (chatId.startsWith('0')) chatId = '62' + chatId.substring(1);
                if (!chatId.endsWith('@c.us')) chatId += '@c.us';

                try {
                    await client.sendMessage(chatId, item.message);
                } catch (e) { }

                if (i < arr.length - 1) {
                    const delayMs = Math.floor(Math.random() * (62000 - 7000 + 1)) + 7000;
                    await new Promise(r => setTimeout(r, delayMs));
                }
            }
        }
    } catch (err) {
        console.error(`❌ Gagal exec jadwal [${job.id}]:`, err);
    }
}

// Background loop to check schedules
setInterval(async () => {
    if (!isReady || !client) return;
    const now = Date.now();
    // Get all schedules that are due and not marked as processing/done
    const pendingJobs = schedules.filter(s => s.status === 'pending' && s.timeToProcess <= now);
    if (pendingJobs.length === 0) return;

    for (const job of pendingJobs) {
        job.status = 'processing';
        saveSchedules(schedules);

        await processSchedule(job);

        // After finish, remove from schedules
        schedules = schedules.filter(s => s.id !== job.id);
        saveSchedules(schedules);
    }
}, 10000); // Check every 10 seconds

// ===== API Routes =====

// Status (QR + ready)
app.get('/api/status', (req, res) => {
    res.json({ ready: isReady, qr: latestQrDataUrl });
});

// Login with Phone Number (Pairing Code)
app.post('/api/login/pair', async (req, res) => {
    if (!client) return res.status(400).json({ error: 'Client belum terinisiasi.' });
    if (isReady) return res.status(400).json({ error: 'Sudah login.' });
    try {
        const { phone } = req.body;
        if (!phone) return res.status(400).json({ error: 'Nomor HP wajib diisi.' });

        let formattedPhone = String(phone).replace(/\D/g, '');
        if (formattedPhone.startsWith('0')) formattedPhone = '62' + formattedPhone.substring(1);

        console.log('📱 Meminta pairing code untuk:', formattedPhone);
        const code = await client.requestPairingCode(formattedPhone);
        res.json({ code });
    } catch (err) {
        console.error('❌ Gagal request pairing code:', err);
        res.status(500).json({ error: 'Gagal: ' + err.message });
    }
});

// Get all lists
app.get('/api/lists', (req, res) => {
    res.json(lists);
});

// ===== Schedules API =====
app.get('/api/schedules', (req, res) => {
    res.json(schedules);
});

app.post('/api/schedules', (req, res) => {
    const { time, payload, type } = req.body;
    if (!time || !payload || !type) return res.status(400).json({ error: 'Data jadwal tidak lengkap' });

    const newSchedule = {
        id: Date.now().toString(),
        timeToProcess: time,
        type: type, // 'grup' or 'pribadi'
        payload: payload,
        status: 'pending',
        createdAt: new Date().toLocaleString('id-ID')
    };

    schedules.push(newSchedule);
    saveSchedules(schedules);
    res.json({ ok: true });
});

app.delete('/api/schedules/:id', (req, res) => {
    const id = req.params.id;
    schedules = schedules.filter(s => s.id !== id);
    saveSchedules(schedules);
    res.json({ ok: true });
});

// Get default message template
app.get('/api/default-message', (req, res) => {
    res.json({ message: buildDefaultMessage() });
});

// Add / update a list (also used for edit)
app.post('/api/lists', (req, res) => {
    const { name, csv } = req.body;
    if (!name || !csv) return res.status(400).json({ error: 'Nama dan CSV wajib diisi' });

    const lines = csv.trim().split('\n');
    if (lines.length < 2) return res.status(400).json({ error: 'CSV harus punya header + minimal 1 baris data' });

    const headers = lines[0].split(',').map(h => h.trim());
    const data = [];
    for (let i = 1; i < lines.length; i++) {
        const cols = lines[i].split(',').map(c => c.trim());
        if (cols.length < headers.length) continue;
        const obj = {};
        headers.forEach((h, idx) => { obj[h] = cols[idx]; });
        data.push(obj);
    }

    lists[name] = data;
    saveLists(lists);
    res.json({ ok: true });
});

// Delete a list
app.delete('/api/lists/:name', (req, res) => {
    const { name } = req.params;
    if (!lists[name]) return res.status(404).json({ error: 'List tidak ditemukan' });
    delete lists[name];
    saveLists(lists);
    res.json({ ok: true });
});

// ===== Targets Routes =====
app.get('/api/targets', (req, res) => {
    res.json(targets);
});

app.post('/api/targets', (req, res) => {
    const { id, label } = req.body;
    if (!id) return res.status(400).json({ error: 'ID tujuan wajib diisi' });
    // Format: nomor biasa -> nomor@c.us, grup -> tetap
    let formattedId = id.trim();
    if (!formattedId.includes('@')) {
        formattedId = formattedId + '@c.us';
    }
    // Cek duplikat
    if (targets.find(t => t.id === formattedId)) {
        return res.status(400).json({ error: 'Target sudah ada di daftar' });
    }
    targets.push({ id: formattedId, label: (label || formattedId).trim() });
    saveTargets(targets);
    res.json({ ok: true });
});

app.delete('/api/targets/:index', (req, res) => {
    const idx = parseInt(req.params.index);
    if (isNaN(idx) || idx < 0 || idx >= targets.length) {
        return res.status(400).json({ error: 'Index tidak valid' });
    }
    targets.splice(idx, 1);
    saveTargets(targets);
    res.json({ ok: true });
});

// Send message to selected targets
app.post('/api/send', async (req, res) => {
    if (!isReady) {
        return res.status(400).json({ error: 'WhatsApp Client belum ready.' });
    }
    // Filter by selected indices if provided
    const selectedIndices = req.body.selectedTargets; // array of indices
    let sendTargets;
    if (Array.isArray(selectedIndices) && selectedIndices.length > 0) {
        sendTargets = selectedIndices.map(i => targets[i]).filter(Boolean);
    } else {
        sendTargets = targets;
    }
    if (sendTargets.length === 0) {
        return res.status(400).json({ error: 'Tidak ada target yang dipilih.' });
    }
    try {
        let userMessage = req.body.message || '';
        let finalMessage = userMessage;
        const mentionIds = []; // string IDs like '628xxx@c.us'
        const hideTag = req.body.hideTag === true;

        for (const [key, list] of Object.entries(lists)) {
            const regex = new RegExp(`\\$\\{${key}\\}`, 'g');
            if (regex.test(userMessage)) {
                finalMessage = finalMessage.replace(new RegExp(`\\$\\{${key}\\}`, 'g'), processList(list));
                mentionIds.push(...getMentionsList(list));
            }
        }

        // Hide Tag: tambahkan semua anggota grup sebagai mentions tanpa teks @
        if (hideTag) {
            for (const target of sendTargets) {
                if (target.id.endsWith('@g.us')) {
                    try {
                        const chat = await client.getChatById(target.id);
                        const participants = chat.participants || [];
                        for (const p of participants) {
                            mentionIds.push(p.id._serialized);
                        }
                        console.log('👻 Hide tag: ' + participants.length + ' anggota dari ' + target.label);
                    } catch (e) {
                        console.log('  ⚠️ Gagal ambil anggota grup untuk hide tag:', target.id);
                    }
                }
            }
        }

        // Resolve Contact objects for mentions (whatsapp-web.js butuh Contact, bukan string)
        const uniqueIds = [...new Set(mentionIds)];
        const mentionContacts = [];
        for (const cid of uniqueIds) {
            try {
                const contact = await client.getContactById(cid);
                mentionContacts.push(contact);
            } catch (e) {
                console.log('  ⚠️ Skip mention (kontak tidak ditemukan):', cid);
            }
        }

        console.log('🚀 Mengirim pesan ke ' + sendTargets.length + ' target...');
        console.log('📝 Pesan:', finalMessage.substring(0, 100) + '...');
        console.log('👤 Mentions:', mentionContacts.length, '/', uniqueIds.length, 'kontak resolved' + (hideTag ? ' (HIDE TAG)' : ''));
        const results = [];
        for (const target of sendTargets) {
            try {
                if (mentionContacts.length > 0) {
                    await client.sendMessage(target.id, finalMessage, { mentions: mentionContacts });
                } else {
                    await client.sendMessage(target.id, finalMessage);
                }
                results.push({ id: target.id, ok: true });
                console.log('  ✅ Terkirim ke ' + target.label);
            } catch (e) {
                const errMsg = typeof e === 'string' ? e : (e && e.message ? e.message : JSON.stringify(e));
                results.push({ id: target.id, ok: false, error: errMsg });
                console.log('  ❌ Gagal ke ' + target.label + ': ' + errMsg);
            }
        }

        const allOk = results.every(r => r.ok);
        const sent = results.filter(r => r.ok).length;
        console.log('🎉 Selesai: ' + sent + '/' + sendTargets.length + ' berhasil.');
        res.json({ ok: allOk, sent, total: sendTargets.length, results });
    } catch (err) {
        console.error('❌ Error route send:', err);
        res.status(500).json({ error: 'Terjadi kesalahan sistem: ' + err.message });
    }
});

// Send private message to a single number
app.post('/api/send-private', async (req, res) => {
    if (!isReady) return res.status(400).json({ error: 'WhatsApp Client belum ready.' });
    try {
        const { target, message } = req.body;
        if (!target || !message) return res.status(400).json({ error: 'Target dan message wajib diisi.' });

        // Ensure format is e.g. 628xxx@c.us
        let chatId = String(target).replace(/\D/g, '');
        if (chatId.startsWith('0')) chatId = '62' + chatId.substring(1);
        if (!chatId.endsWith('@c.us')) chatId += '@c.us';

        console.log(`🚀 [Private] Mengirim ke ${chatId}...`);
        try {
            await client.sendMessage(chatId, message);
            res.json({ ok: true, id: chatId });
        } catch (e) {
            const errMsg = typeof e === 'string' ? e : (e?.message || JSON.stringify(e));
            console.log(`❌ [Private] Gagal ke ${chatId}: ${errMsg}`);
            res.status(500).json({ error: errMsg });
        }
    } catch (err) {
        res.status(500).json({ error: 'Terjadi kesalahan: ' + err.message });
    }
});

// ===== Group Members Route =====
app.get('/api/group-members/:groupId', async (req, res) => {
    if (!isReady) {
        return res.status(400).json({ error: 'WhatsApp Client belum ready.' });
    }
    try {
        const groupId = decodeURIComponent(req.params.groupId);
        const chat = await client.getChatById(groupId);
        if (!chat.isGroup) {
            return res.status(400).json({ error: 'ID ini bukan grup.' });
        }

        const participants = chat.participants || [];
        const members = [];

        for (const p of participants) {
            let nomor = p.id._serialized.replace('@c.us', '');
            // Pastikan dimulai dari 62
            if (nomor.startsWith('0')) {
                nomor = '62' + nomor.substring(1);
            } else if (!nomor.startsWith('62')) {
                nomor = '62' + nomor;
            }

            let nama = nomor; // default: nomor sebagai nama
            try {
                const contact = await client.getContactById(p.id._serialized);
                if (contact.pushname) nama = contact.pushname;
                else if (contact.name) nama = contact.name;
                else if (contact.shortName) nama = contact.shortName;
            } catch (e) { }

            members.push({
                nama: nama,
                nomor: nomor,
                isAdmin: p.isAdmin || false,
                isSuperAdmin: p.isSuperAdmin || false
            });
        }

        // Sort: super admin dulu, lalu admin, lalu biasa
        members.sort((a, b) => {
            if (a.isSuperAdmin && !b.isSuperAdmin) return -1;
            if (!a.isSuperAdmin && b.isSuperAdmin) return 1;
            if (a.isAdmin && !b.isAdmin) return -1;
            if (!a.isAdmin && b.isAdmin) return 1;
            return a.nama.localeCompare(b.nama);
        });

        console.log(`👥 Fetched ${members.length} anggota dari grup ${groupId}`);
        res.json({ groupName: chat.name, members });
    } catch (err) {
        console.error('❌ Gagal ambil anggota grup:', err);
        res.status(500).json({ error: 'Gagal mengambil anggota grup: ' + err.message });
    }
});
// ===== Monitor Routes =====
app.get('/api/monitor', (req, res) => {
    res.json({ on: monitorOn, messages: incomingMessages });
});

app.post('/api/monitor/toggle', (req, res) => {
    monitorOn = !monitorOn;
    if (!monitorOn) incomingMessages = [];
    console.log(`🔔 Monitor ${monitorOn ? 'ON' : 'OFF'}`);
    res.json({ on: monitorOn });
});

// ===== Logout Route =====
app.post('/api/logout', async (req, res) => {
    if (!client) {
        return res.status(400).json({ error: 'Client belum terinisiasi.' });
    }
    try {
        console.log('🚪 Memproses logout...');

        // Timeout utility to prevent hanging
        const withTimeout = (promise, ms, name) => {
            return Promise.race([
                promise,
                new Promise((_, reject) => setTimeout(() => reject(new Error(name + ' timeout (' + ms + 'ms)')), ms))
            ]);
        };

        if (isReady) {
            try {
                await withTimeout(client.logout(), 10000, 'Logout');
                console.log('✅ Berhasil kirim command logout ke WhatsApp.');
            } catch (e) {
                console.log('⚠️ Gagal kirim command logout (mungkin frame tertutup). Lanjut destroy...', e.message);
            }
        }

        isReady = false;
        latestQrDataUrl = null;
        monitorOn = false;
        incomingMessages = [];

        // Destroy client
        try {
            await withTimeout(client.destroy(), 10000, 'Destroy');
            console.log('✅ Client berhasil di-destroy.');
        } catch (e) {
            console.log('⚠️ Error saat destroy client:', e.message);
        }

        // Force cleanup LocalAuth folder to ensure new QR is generated
        try {
            const fs = require('fs');
            const authPath = './.wwebjs_auth';
            if (fs.existsSync(authPath)) {
                fs.rmSync(authPath, { recursive: true, force: true });
                console.log('🧹 Folder .wwebjs_auth berhasil dihapus secara paksa.');
            }
        } catch (e) {
            console.log('⚠️ Gagal menghapus folder auth:', e.message);
        }

        setTimeout(() => {
            console.log('🔄 Re-initializing client for new QR...');
            createClient();
        }, 1000);

        res.json({ ok: true });
    } catch (err) {
        console.error('❌ Terjadi error fatal saat logout:', err);
        res.status(500).json({ error: 'Gagal memproses logout: ' + err.message });
    }
});

// ===== Start =====
app.listen(port, () => {
    console.log(`🌐 Web interface berjalan di http://localhost:${port}`);
});

createClient();