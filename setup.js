const readline = require('readline');
const fs = require('fs');
const path = require('path');

const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout
});

const envPath = path.join(__dirname, '.env');
let envContent = '';
if (fs.existsSync(envPath)) {
    envContent = fs.readFileSync(envPath, 'utf8');
}

console.log('=============================================');
console.log(' WhatsApp Bot Setup - Konfigurasi Auto Email ');
console.log('=============================================\n');
console.log('💡 Catatan: Google tidak mengizinkan pembuatan Sandi Aplikasi (App Password) lewat script.');
console.log('Anda harus membuatnya sendiri di akun Google Anda (Keamanan -> Sandi Aplikasi).');
console.log('Script ini hanya membantu menuliskan password ke konfigurasi sistem secara otomatis.\n');

rl.question('➤ Masukkan Email (contoh: anugrahsahabatkita@gmail.com): ', (email) => {
    rl.question('➤ Masukkan 16-digit Sandi Aplikasi Gmail: ', (pass) => {
        rl.question('➤ Masukkan URL/Link web server ini (opsional, tekan Enter untuk lewati): ', (url) => {
            const newEnv = `EMAIL_USER=${email}\nEMAIL_PASS=${pass.replace(/\s+/g,'')}\nPUBLIC_URL=${url || 'http://localhost:3000'}\n`;
            
            fs.writeFileSync(envPath, newEnv, 'utf8');
            console.log('\n✅ Sukses! File konfigurasi (.env) telah dibuat.');
            console.log('Aplikasi sekarang sudah siap mengirimkan email notifikasi saat ter-logout.');
            rl.close();
        });
    });
});
