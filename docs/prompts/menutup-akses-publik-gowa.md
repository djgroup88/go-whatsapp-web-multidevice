# Menutup Akses Publik GOWA (Fix IP & Session Leak)

## Context

Aplikasi GOWA (`go-whatsapp-web-multidevice`) dijalankan via Docker di server `62.72.46.219`
dan port-nya (`5002`) **dipublikasikan ke semua interface** (`0.0.0.0`). Akibatnya siapa pun
yang scan/dorking internet bisa membuka `http://62.72.46.219:5002/`, melihat halaman + session
WhatsApp, dan IP asli server ikut bocor.

Ada **dua akar masalah**:

1. **Port Docker terekspos publik.** Docker mem-publish port dengan menulis rule iptables
   sendiri yang **melewati (bypass) firewall `ufw`** — jadi walaupun ufw aktif, `5002` tetap
   terbuka ke internet. (kode: `docker-compose.yml:12` `ports: "3000:3000"`; default
   `APP_HOST=0.0.0.0` di `src/config/settings.go:12`)
2. **Basic auth kemungkinan mati atau masih pakai kredensial placeholder.** Auth GOWA off
   secara default; hanya aktif jika `APP_BASIC_AUTH` di-set (kode: `src/cmd/rest.go:121-134`).
   File `src/.env` yang ter-commit berisi placeholder `user1:pass1,user2:pass2` — itu bukan
   proteksi karena kredensialnya publik di repo.

User memakai **Nginx Proxy Manager (NPM) + Cloudflare** untuk `gowa.djgroupapp.com`, dan hanya
ingin **menutup akses** (tidak perlu langkah re-pair session).

> Catatan: semua perubahan di bawah dilakukan **di server** (file `docker-compose.yml` / `.env`
> milik user di server, panel NPM, dan dashboard Cloudflare). Tidak ada perubahan kode di repo
> ini yang diperlukan.

---

## Langkah Perbaikan

### 1. Jangan publish port ke publik — biarkan hanya lewat NPM (fix paling penting)

Tujuan: `62.72.46.219:5002` tidak lagi bisa diakses dari luar; akses hanya via
`https://gowa.djgroupapp.com` melalui NPM.

Ada dua skenario tergantung NPM user:

**A. NPM juga jalan sebagai container Docker (paling umum) — cara paling bersih:**
Hapus publikasi port dan hubungkan GOWA + NPM ke satu Docker network, lalu NPM forward ke
nama container.

Di `docker-compose.yml` GOWA:
```yaml
services:
  whatsapp_go:
    # HAPUS blok ports: publik ini
    # ports:
    #   - "5002:3000"
    networks:
      - npm_network        # network yang sama dengan Nginx Proxy Manager

networks:
  npm_network:
    external: true          # network milik stack NPM
```
Lalu di NPM Proxy Host untuk `gowa.djgroupapp.com`, arahkan:
- Forward Hostname/IP: `whatsapp_go` (nama service/container)
- Forward Port: `3000` (port internal container, bukan 5002)

**B. NPM native / di host (bukan container):**
Bind port hanya ke loopback supaya tidak terekspos publik tapi NPM lokal tetap bisa akses:
```yaml
    ports:
      - "127.0.0.1:5002:3000"   # sebelumnya "5002:3000"
```
NPM tetap forward ke `127.0.0.1:5002`.

Verifikasi setelah `docker compose up -d`:
```bash
docker compose ps        # cek mapping port
ss -tlnp | grep 5002     # skenario A: tidak muncul; skenario B: hanya 127.0.0.1:5002
```

### 2. Aktifkan Basic Auth yang kuat (defense-in-depth)

Set kredensial acak yang panjang di `src/.env` (bukan placeholder repo):
```env
APP_BASIC_AUTH=admingowa:GANTI_password_acak_panjang_disini
```
(Bisa banyak kredensial dipisah koma: `user1:passA,user2:passB`.)
Buat password acak: `openssl rand -base64 24`
Format & enforcement: `src/cmd/root.go:96-99`, `src/cmd/rest.go:121-134`.
Restart container agar terbaca: `docker compose up -d`.

Catatan: endpoint `GET /health` dan (jika Chatwoot aktif) `POST /chatwoot/webhook` tetap
tanpa auth by design (`src/cmd/rest.go:88-118`) — itu wajar.

### 3. Sembunyikan IP asli di Cloudflare

Karena IP sudah terlanjur bocor, minimal cegah kebocoran berulang & saring trafik:
- DNS record `gowa.djgroupapp.com` → set **Proxied (awan oranye)**, bukan DNS-only, agar IP
  origin `62.72.46.219` disembunyikan di balik Cloudflare.
- SSL/TLS mode: **Full (strict)** (NPM sudah pegang sertifikat Let's Encrypt untuk domain).
- (Opsional) Cloudflare → Security/WAF: batasi akses, mis. rule country/IP allowlist atau
  Cloudflare Access, karena panel ini sangat sensitif.

### 4. (Opsional) Pertegas trusted proxies

Di `src/.env`, `APP_TRUSTED_PROXIES` saat ini `0.0.0.0/0` (percaya semua proxy). Setelah
akses hanya lewat NPM, ini boleh dipersempit ke subnet Docker/NPM saja (mis.
`172.16.0.0/12`) supaya header `X-Forwarded-For` tidak bisa dipalsukan. Bukan wajib untuk
menutup leak, tapi baik untuk kebersihan. (kode: `src/config/settings.go:18`)

---

## Verifikasi (end-to-end)

Setelah semua diterapkan dan `docker compose up -d`:

1. **Port publik tertutup** — dari mesin lain / HP pakai data seluler (bukan wifi kantor):
   - Buka `http://62.72.46.219:5002/` → harus **timeout / connection refused** (tidak terbuka).
   - Atau `nc -zv 62.72.46.219 5002` → harus gagal / refused.
2. **Domain tetap jalan + minta login** — buka `https://gowa.djgroupapp.com` → muncul prompt
   Basic Auth; kredensial placeholder `user1:pass1` **ditolak**, kredensial baru diterima.
3. **IP tersembunyi** — `nslookup gowa.djgroupapp.com` / `dig gowa.djgroupapp.com` → yang muncul
   IP Cloudflare (mis. `104.x` / `172.6x`), bukan `62.72.46.219`.

Jika ketiganya lolos, panel tidak lagi bisa ditemukan lewat scan IP:port dan wajib login lewat
domain ber-TLS.
