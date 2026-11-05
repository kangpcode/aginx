# PRD — AGinX Control Panel

| Field | Nilai |
|---|---|
| Nama produk | **AGinX Control Panel** (`aginx-panel`) |
| Versi dokumen | 1.0 — Draft untuk implementasi |
| Tanggal | 5 November 2026 |
| Pemilik produk | Dhafa Nazula Permadi (kangpcode) |
| Engine | AGinX native (Go, `.ginxaccess`) |
| Referensi UX | aaPanel 8.x, OpenPanel 1.x |
| Status | **Approved untuk scaffolding; menunggu keputusan pada §18** |

> **Cara memakai dokumen ini.** PRD ini adalah *single source of truth* sekaligus prompt eksekusi. Berikan seluruh file ini kepada agen coding atau tim, lalu minta mereka mengerjakan sesuai urutan milestone di §14. Setiap keputusan teknis sudah disertai alasan dan trade-off. Bagian yang masih terbuka ditandai eksplisit di §18; jangan mengarang jawaban, gunakan asumsi yang tercatat di §3.4.

---

## 1. Ringkasan Eksekutif

### 1.1 Masalah

AGinX adalah web server native dengan keunggulan teknis (event-driven, `.ginxaccess` per direktori, cache konfigurasi, tanpa reload). Namun tanpa panel, operasionalnya menyentuh file konfigurasi, sinyal proses, permission, sertifikat, dan log secara manual. Pengguna yang terbiasa dengan aaPanel/OpenPanel mengharapkan: buat situs dalam 3 klik, pasang SSL, kelola file, lihat trafik, backup, dan kelola pengguna — tanpa SSH.

Panel generik (aaPanel, OpenPanel, CyberPanel, HestiaCP) tidak dapat mengekspos keunggulan AGinX karena mereka mengasumsikan Nginx/Apache/OpenLiteSpeed dan `.htaccess` yang di-parse ulang tiap request. Tidak ada panel yang memahami `.ginxaccess`, inheritance-nya, atau semantik keamanan ACL parent→child AGinX.

### 1.2 Solusi

**AGinX Control Panel** adalah panel native, single-node, yang:

1. Mengelola siklus hidup situs, domain, TLS, file, database, cron, firewall, backup, dan pengguna.
2. Menjadikan **`.ginxaccess` sebagai warga kelas satu**: editor dengan validasi engine asli, *lint* per baris, *rule simulator*, visualisasi inheritance, riwayat revisi, diff, publish atomik, dan rollback.
3. Berjalan dengan **privilege separation**: proses web non-root + agen root dengan RPC allowlist, bukan shell bebas.
4. Tetap **satu binary statis** (UI di-embed), tanpa Docker, tanpa runtime PHP wajib, tanpa ketergantungan Nginx/Apache.

### 1.3 Diferensiasi utama

| Kapabilitas | aaPanel | OpenPanel | AGinX Panel |
|---|---|---|---|
| Config per direktori | `.htaccess` (Apache) / rewrite Nginx | Tidak diekspos | **`.ginxaccess` native + cache + inheritance** |
| Validasi config sebelum publish | Terbatas / reload | Tidak ada | **Validasi engine + diff + rollback otomatis** |
| Simulasi request terhadap aturan | Tidak | Tidak | **Ya, memakai evaluator yang sama dengan engine** |
| Zero-downtime apply | Reload Nginx | Restart container | **Publish atomik + reload graceful** |
| Model privilege | Proses web sebagai root | Docker | **Web non-root + agen root ber-allowlist** |
| Kebutuhan runtime | Nginx/Apache + PHP | Docker | **Satu binary native** |

### 1.4 Definisi sukses (produk)

| Metrik | Target |
|---|---|
| Time-to-first-site (install → situs HTTPS aktif) | ≤ 5 menit |
| Publish `.ginxaccess` (validasi → aktif) | p95 ≤ 2 detik, 0 downtime |
| Publish config yang gagal validasi | 100% ditolak, versi lama tetap aktif |
| Dashboard first contentful paint (LAN) | ≤ 1,2 detik |
| API p95 (localhost, 100 situs) | ≤ 150 ms |
| RSS idle panel web + agen | ≤ 140 MB gabungan |
| Insiden kehilangan data konfigurasi | 0 |
| Temuan critical/high pada audit keamanan rilis | 0 |
| Crash-free session | ≥ 99,9% |

---

## 2. Konteks dan Benchmark Produk

### 2.1 aaPanel — yang diadopsi

Kekuatan yang terbukti disukai pengguna:

- **Sidebar kiri padat** dengan label ikon: Dashboard, Website, FTP, Database, Monitor, Security, Files, Terminal, Cron, App Store, Settings.
- **Dashboard kartu** berisi CPU/RAM/disk/load, status layanan, dan tombol cepat.
- **Website manager** dengan daftar situs, modal tambah situs, tab per situs (domain, SSL, rewrite, log, backup, database).
- **File manager** dua panel, editor kode, unggah drag-and-drop, arsip.
- **App Store** untuk instalasi satu klik (WordPress, PHP, database).
- **Notifikasi lonceng** + panel pembaruan.

Kelemahan yang **tidak** akan ditiru:

- Proses panel berjalan sebagai `root` dan mengeksekusi shell string.
- Banyak fitur bergantung pada Nginx/Apache dan reload penuh.
- Config situs tersebar antara file Nginx, `.htaccess`, dan metadata panel.
- UI padat namun tidak konsisten; modal bersarang dan state sulit diprediksi.

### 2.2 OpenPanel — yang diadopsi

- **UI modern** dengan sidebar terang/gelap, kartu ringkas, dan form drawer.
- **Multi-tenant**: setiap akun punya domain, website, email, database, dan kuota.
- **Onboarding wizard** dan navigasi berbasis domain (bukan berbasis server).
- **Terminal, cron, backup, firewall** terintegrasi dalam satu shell UI.

Kelemahan yang **tidak** akan ditiru:

- Ketergantungan Docker untuk isolasi membuat footprint besar dan sulit diaudit di VPS kecil.
- Abstraksi container menyembunyikan konfigurasi web server; engineer tidak bisa melihat efek akhir.
- Scope luas (email, DNS) menambah permukaan serangan dan beban operasional.

### 2.3 Keputusan adopsi

| Aspek | Adopsi | Adaptasi untuk AGinX |
|---|---|---|
| Layout sidebar + topbar | aaPanel | Sidebar 248 px dapat dikuncupkan, topbar 56 px, konten maksimal 1440 px |
| Kartu dashboard | aaPanel | Ditambah kartu engine (RPS, koneksi, cache) dan kartu *config health* |
| Wizard tambah situs | aaPanel | 3 langkah, dengan preview file config yang akan ditulis |
| Tab per situs | aaPanel | 8 tab, termasuk **Config Studio** dan **Revisions** |
| Tema terang/gelap | OpenPanel | Token warna ganda, menghormati `prefers-color-scheme` |
| Form drawer | OpenPanel | Drawer kanan 480 px untuk create/edit, halaman penuh untuk editor |
| Multi-tenant | OpenPanel | v1.0: kepemilikan situs per pengguna panel; isolasi OS per situs di §9.2 |
| Command palette | OpenPanel | `Ctrl/⌘ + K` untuk navigasi dan aksi cepat |
| Email/DNS/Docker | — | **Out of scope** (§4), dengan alasan eksplisit |

### 2.4 Posisi produk

> Panel untuk engineer yang ingin kecepatan panel modern **tanpa** kehilangan kontrol atas web server. Konfigurasi tetap file teks yang dapat di-audit, tetapi divalidasi, disimulasikan, dan dipublikasikan dengan aman.

---

## 3. Pengguna, Asumsi, dan Batasan

### 3.1 Persona

| Persona | Kebutuhan utama | Rasa sakit saat ini |
|---|---|---|
| **Solo developer / indie hacker** | Deploy situs statis/API dalam menit, SSL otomatis, backup | Menyunting Nginx manual, lupa reload, cert kedaluwarsa |
| **Sysadmin agensi kecil** | Kelola 10–200 situs, delegasi per klien, audit perubahan | Panel lain mengeksekusi shell sebagai root, sulit dipertanggungjawabkan |
| **Engineer AGinX awal** | Menguji `.ginxaccess` tanpa menebak, melihat hasil merge | Tidak ada alat untuk simulasi dan diff aturan |
| **Operator keamanan** | Firewall, rate limit, log, jejak audit | Konfigurasi tersebar, tidak ada riwayat siapa mengubah apa |

### 3.2 Jobs to be done

1. “Saat saya punya domain baru, saya ingin situs + HTTPS aktif tanpa menyentuh SSH.”
2. “Saat saya mengubah aturan rewrite/ACL, saya ingin tahu dampaknya sebelum menyentuh produksi.”
3. “Saat situs diserang, saya ingin memblokir IP/UA dan menaikkan rate limit dalam < 1 menit.”
4. “Saat ada insiden, saya ingin tahu siapa mengubah config, kapan, dan isinya.”
5. “Saat server penuh, saya ingin melihat penyebabnya dan memindahkan backup.”

### 3.3 Batasan yang mengikat

- Target OS: Debian 12/13, Ubuntu 22.04/24.04, AlmaLinux 9, Rocky Linux 9; kernel ≥ 4.18; `amd64` dan `arm64`.
- Deployment: single node dengan akses root dan systemd. Multi-node bukan v1.
- Panel harus tetap dapat dioperasikan **tanpa koneksi internet** (kecuali penerbitan ACME dan update).
- Tidak boleh memerlukan Docker, Node.js, PHP, atau Nginx di server produksi.
- Semua operasi privileged harus melalui agen dengan allowlist; **tidak ada** `sh -c` yang dibangun dari input pengguna.
- Kompatibilitas config mengikuti spec `.ginxaccess` v1 di `docs/03-ginxaccess.md`; panel tidak boleh menciptakan sintaks di luar spec tanpa memperbarui engine.

### 3.4 Asumsi eksplisit

| # | Asumsi | Dampak jika salah |
|---|---|---|
| A1 | Engine AGinX v1.0 akan menyediakan multi-site (`server` block), TLS/SNI, FastCGI, admin socket, dan metrics | Panel memakai adapter + engine mock; milestone §14 bergeser |
| A2 | Panel diakses lewat SSH tunnel/localhost secara default | Jika publik, wajib TLS + 2FA + IP allowlist (sudah jadi default aman) |
| A3 | Stack: Go stdlib + SQLite pure-Go, frontend React/TypeScript | Jika ingin tanpa Node, UI dapat diganti htmx tanpa mengubah API |
| A4 | Lisensi repo/produk belum ditentukan | Jangan menambahkan header lisensi; tunggu keputusan pemilik |
| A5 | Satu panel mengelola satu server; “node” di UI hanya satu | Desain API tetap menyertakan `node_id` agar multi-node tidak merusak kontrak |

---

## 4. Ruang Lingkup dan Non-Goals

### 4.1 Matriks modul

Legenda: **●** penuh · **◐** terbatas · **–** tidak ada.

| Modul | MVP (v0.9) | v1.0 | v1.1 | v2.0 |
|---|---|---|---|---|
| Dashboard | ● dasar | ● | ● | ● |
| Websites | ● static | ● | ● reverse proxy | ● |
| Config Studio `.ginxaccess` | ● editor + publish + revisi + simulator dasar | ● inheritance + analisis dampak | ● form builder | ● |
| SSL/TLS | ◐ upload + ACME | ● auto-renew + force HTTPS | ● DNS-01 | ● |
| Files | ◐ browse/upload/edit | ● arsip + permission + trash | ● quota | ● |
| Databases | – | ● MySQL/MariaDB | ● PostgreSQL | ● |
| Cron | – | ● | ● | ● |
| Terminal | – | ● | ● recording | ● |
| Security/Firewall | ◐ status | ● nftables/ufw | ● fail2ban native | ● |
| Logs | ● tail + filter dasar | ● rotate + unduh teredaksi | ● export | ● |
| Monitoring | ◐ sistem | ● + engine metrics | ● alerting | ● |
| Backup | – | ◐ lokal | ● S3/SFTP | ● |
| Users/RBAC | ◐ 4 role + 2FA | ● membership per situs | ● | ● |
| Settings | ● | ● | ● | ● |
| Notifications | – | ● in-app + email | ● webhook | ● |
| Extensions/App catalog | – | – | – | ● |

### 4.2 Non-goals (v1.0)

| Tidak dibangun | Alasan |
|---|---|
| **Mail server** (SMTP/IMAP, antispam) | Bukan domain web server; deliverability, abuse handling, dan reputasi IP adalah produk terpisah. Panel hanya mengirim email notifikasi via SMTP relay. |
| **Authoritative DNS server** | Mengelola zone DNS membutuhkan server terpisah dan model keamanan berbeda; integrasi DNS API pihak ketiga dapat ditambahkan v2. |
| **Docker/container manager** | Bertentangan dengan model native single-binary dan menambah footprint. |
| **Billing/reseller/shared hosting** | Panel adalah alat operasional, bukan platform komersial. |
| **PHP version manager** | Menunggu FastCGI stabil di engine; panel hanya mendaftarkan upstream. |
| **Multi-node/cluster** | API disiapkan, implementasi ditunda. |
| **Editor visual drag-and-drop penuh** | Config tetap berbasis teks; form menghasilkan snippet, bukan AST dua arah. |

---

## 5. Prinsip Produk

1. **File adalah sumber kebenaran.** Semua yang ditampilkan panel harus dapat direkonstruksi dari file di disk. Database panel menyimpan metadata, draft, dan audit — bukan kebenaran konfigurasi.
2. **Validasi sebelum efek.** Tidak ada perubahan yang menyentuh produksi tanpa validasi engine dan diff.
3. **Fail closed.** Config rusak, agen mati, atau engine tidak sehat → perubahan ditolak, versi valid terakhir tetap aktif.
4. **Least privilege.** Web non-root; agen hanya mengeksekusi operasi terdaftar; tidak ada shell string.
5. **Terlihat dan dapat dibatalkan.** Setiap aksi destruktif punya konfirmasi spesifik, preview, dan jalan rollback.
6. **Cepat itu fitur.** Tidak ada spinner tanpa progres; daftar besar dipaginasi server-side; aksi < 2 detik memberi feedback optimistis.
7. **Konsisten, bukan ramai.** Satu pola halaman, satu pola form, satu pola tabel.

---

## 6. Information Architecture dan Layout

### 6.1 Peta navigasi lengkap

`Status` mengikuti §4.1. Route memakai React Router; semua route privat di bawah `AppShell`.

```text
/login                                AUTH  publik
/setup                                AUTH  hanya saat belum ada user
/                                     → /dashboard

/dashboard                            ●     ringkasan server + engine + situs
/sites                                ●     daftar situs, filter, bulk action
/sites/new                            ●     wizard 3 langkah
/sites/:siteId                        ●     layout tab
  ├── overview                        ●     ringkasan, domain, status, quick actions
  ├── domains                         ●     kelola domain + redirect
  ├── config                          ●     Config Studio (editor)
  ├── config/revisions                ●     riwayat, diff, rollback
  ├── config/simulator                ●     uji request terhadap aturan
  ├── ssl                             ●     sertifikat, auto-renew, force HTTPS
  ├── files                           ●     file manager ter-scope ke docroot
  ├── logs                            ●     access/error situs
  ├── backups                         ●     backup/restore situs
  └── metrics                         ●     RPS, bandwidth, status class

/config                               ●     daftar semua config + health
/domains                              ●     daftar domain lintas situs
/ssl                                  ●     inventaris sertifikat + expiry
/files                                ●     pemilih situs → file manager
/databases                            ●     MySQL/MariaDB
/cron                                 ●     cron per situs/user
/terminal                             ●     terminal web (role-gated)
/security                             ●     tab: firewall, login protection, WAF preset
/logs                                 ●     tab: situs, engine, panel, auth, jobs
/monitor                              ●     sistem + engine + per situs
/backups                              ●     job, jadwal, retensi
/users                                ●     pengguna panel, role, membership
/settings                             ●     tab: umum, engine, keamanan, notifikasi, update, token
/profile                              ●     akun sendiri, 2FA, sesi
/extensions                           v2    katalog ekstensi
/apps                                 v2    katalog aplikasi (WordPress, Laravel)
```

### 6.2 Shell layout

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ ☰  AGinX        [ aginx-01 ▾ ]      [ ⌘K  Cari atau lompat… ]   ● Sehat  🔔  ◐  ▾ │ 56px
├───────────────────┬──────────────────────────────────────────────────────────┤
│  ◉ Dashboard      │  Dashboard                                   [+ Situs]    │
│                   │  Ringkasan server · 5 Nov 2026 14:32 WIB                  │
│  SITUS            │  ┌──────────────┬──────────────┬──────────────┬─────────┐  │
│  ▣ Websites   12  │  │ CPU 18%      │ RAM 3,1/8 GB │ Disk 62%     │ Load    │  │
│  ⌘ Domains    18  │  │ ▁▂▃▅▃▂       │ ███░░░░      │ ██████░░     │ 0.42    │  │
│  ⚙ Config      3! │  └──────────────┴──────────────┴──────────────┴─────────┘  │
│  🔒 SSL        2! │  ┌────────────────────────────┬──────────────────────────┐ │
│  ▤ Files          │  │ AGinX Engine               │ Situs                    │ │
│  ▦ Databases  4   │  │ ● running  v1.0.0  12d     │ 12 aktif · 2 peringatan  │ │
│  ⏱ Cron        9  │  │ 2.4k RPS · 318 conn        │ example.com      ●       │ │
│                   │  │ cache hit 94,2%            │ api.example.com  ! SSL   │ │
│  SISTEM           │  └────────────────────────────┴──────────────────────────┘ │
│  ◫ Monitor        │  ┌────────────────────────────┬──────────────────────────┐ │
│  ⛨ Security    1! │  │ Trafik 24 jam              │ Aktivitas terbaru        │ │
│  ☰ Logs           │  │ ╭─╮   ╭╮    ╭─╮            │ dhafa publish config…    │ │
│  ⌨ Terminal       │  │ ╯ ╰───╯╰────╯ ╰──          │ backup selesai 14:20     │ │
│  ⛁ Backups        │  └────────────────────────────┴──────────────────────────┘ │
│                   │  ┌────────────────────────────────────────────────────────┐ │
│  PANEL            │  │ Peringatan: sertifikat api.example.com kedaluwarsa 6h  │ │
│  ♟ Users          │  └────────────────────────────────────────────────────────┘ │
│  ⚙ Settings       │                                                          │
└───────────────────┴──────────────────────────────────────────────────────────┘
   248px (64px collapsed)                          konten max-width 1440px
```

Aturan layout:

- Sidebar tetap (fixed), scroll independen, grup dengan label kapital 11 px.
- Badge angka = jumlah item yang butuh perhatian; maksimum tampil `99+`.
- Topbar: pemilih node (disabled “1 node” di v1), command palette, status engine, notifikasi, tema, menu pengguna.
- Header halaman: judul, breadcrumb, deskripsi satu baris, aksi primer di kanan.
- Konten: grid 12 kolom, gutter 24 px, kartu radius 12 px.
- Drawer kanan 480 px untuk create/edit cepat; halaman penuh untuk editor, terminal, dan file manager.
- Mobile (< 768 px): sidebar menjadi drawer, tabel menjadi kartu, aksi sekunder masuk menu `⋯`.

### 6.3 Pola interaksi wajib

| Pola | Aturan |
|---|---|
| Form | Label di atas, bantuan di bawah, error inline di bawah field, simpan sticky footer. Validasi saat blur + submit; tidak ada validasi hanya di server. |
| Tabel | Header sticky, sort, filter chip, pagination server-side, empty state dengan aksi, bulk select dengan action bar. |
| Konfirmasi destruktif | Wajib mengetik nama resource; tampilkan dampak; tombol bahaya tidak auto-focus. |
| Job panjang | Tombol berubah menjadi progress; job tetap berjalan saat navigasi; muncul di panel job global. |
| Toast | Sukses 4 detik, error 8 detik dengan “Lihat detail”, tidak menutupi aksi. |
| Error | Pesan manusiawi + kode + request ID + tombol salin; error validasi config menampilkan file:baris:kolom. |
| Loading | Skeleton untuk konten, spinner hanya untuk aksi < 2 detik. |
| Empty | Ilustrasi ringan, satu kalimat penyebab, satu aksi utama. |
| Unsaved changes | Blokir navigasi, tawarkan simpan draft / buang. |

### 6.4 Design token

| Token | Terang | Gelap |
|---|---|---|
| `--bg-canvas` | `#F6F8FB` | `#0B1220` |
| `--bg-surface` | `#FFFFFF` | `#111A2C` |
| `--bg-sidebar` | `#0F1B2D` | `#070D18` |
| `--text-primary` | `#0F172A` | `#E6EDF7` |
| `--text-secondary` | `#475569` | `#94A3B8` |
| `--border` | `#E2E8F0` | `#1E293B` |
| `--primary` | `#2563EB` | `#3B82F6` |
| `--primary-hover` | `#1D4ED8` | `#60A5FA` |
| `--success` | `#16A34A` | `#22C55E` |
| `--warning` | `#D97706` | `#F59E0B` |
| `--danger` | `#DC2626` | `#EF4444` |
| `--focus-ring` | `#2563EB` 2 px + offset 2 px | `#60A5FA` |

Tipografi: **Inter** (UI), **JetBrains Mono** (kode, log, angka teknis). Skala 12/13/14/16/20/24/32 px; line-height 1.5 body, 1.25 heading. Spasi kelipatan 4 px. Radius 8 px kontrol, 12 px kartu. Shadow hanya untuk elemen mengambang. Ikon 16/20 px, stroke 1,75.

### 6.5 Aksesibilitas dan i18n

- WCAG 2.1 AA: kontras ≥ 4,5:1, target sentuh ≥ 44 px, fokus terlihat, urutan tab logis.
- Semua ikon-only punya `aria-label`; tabel punya caption; chart punya ringkasan teks.
- Tidak ada informasi yang hanya disampaikan lewat warna.
- `prefers-reduced-motion` mematikan animasi non-esensial.
- Bahasa UI: `id-ID` default, `en-US` tersedia. Semua string lewat katalog i18n; tidak ada string hardcoded di komponen.
- Format tanggal/waktu/angka mengikuti locale; timezone server ditampilkan eksplisit.

---

## 7. Spesifikasi Fungsional

Prioritas: **P0** wajib untuk rilis yang dinyatakan, **P1** penting, **P2** nice-to-have. Setiap requirement punya acceptance criteria yang dapat diuji.

### 7.1 Autentikasi dan Onboarding

**Tujuan:** panel aman sejak detik pertama, tanpa ketergantungan email.

| ID | Requirement | Prioritas |
|---|---|---|
| AUTH-01 | First-run wizard `/setup` hanya aktif jika tabel `users` kosong; menolak akses dari non-loopback kecuali `--allow-remote-setup`. | P0 |
| AUTH-02 | Setup meminta username, email, password (min 12 karakter, cek daftar password umum), dan konfirmasi TOTP. | P0 |
| AUTH-03 | Setup token 32 byte dicetak ke stdout dan ditulis ke `/var/lib/aginx-panel/setup.token` (0600), wajib dimasukkan. | P0 |
| AUTH-04 | Login dengan username/email + password + TOTP (jika aktif). Pesan gagal seragam. | P0 |
| AUTH-05 | Lockout: 5 gagal dalam 15 menit per (IP, akun) → tunggu eksponensial 1/2/4/8/15 menit; notifikasi ke admin. | P0 |
| AUTH-06 | Sesi: cookie `aginx_session` HttpOnly, Secure, SameSite=Strict, rotasi saat login, idle 30 menit, absolut 12 jam (remember me 30 hari). | P0 |
| AUTH-07 | CSRF: token per sesi, header `X-CSRF-Token`, validasi untuk semua non-GET. | P0 |
| AUTH-08 | 10 recovery code sekali pakai, ditampilkan sekali, disimpan sebagai hash. | P0 |
| AUTH-09 | Reset password hanya via CLI `aginx-panel admin reset-password <user>`; tidak ada reset via email di v1. | P1 |
| AUTH-10 | Manajemen sesi: daftar perangkat, cabut sesi lain, cabut semua. | P1 |
| AUTH-11 | API token dengan scope (`read`, `write`, `admin`) dan expiry; token ditampilkan sekali. | P1 |
| AUTH-12 | Re-auth (password + TOTP) untuk aksi sensitif: terminal root, hapus situs, ubah firewall, keluarkan sertifikat. | P0 |

**Acceptance:** tidak ada endpoint selain `/api/v1/auth/*` dan `/api/v1/health` yang dapat diakses tanpa sesi; cookie tidak pernah muncul di log; lockout terbukti dengan test.

### 7.2 Dashboard

| ID | Requirement | Prioritas |
|---|---|---|
| DASH-01 | Kartu sistem: CPU %, RAM used/total, swap, disk used/total per mount utama, inode, load 1/5/15. | P0 |
| DASH-02 | Kartu engine: status, versi, uptime, worker, koneksi aktif, RPS 1m, cache hit ratio, error rate 5xx. | P0 |
| DASH-03 | Kartu situs: total, aktif, butuh perhatian (config invalid, cert < 14 hari, engine down). | P0 |
| DASH-04 | Grafik trafik 24 jam (RPS + bandwidth) dengan resolusi 1 menit; hover menampilkan nilai. | P0 |
| DASH-05 | Feed aktivitas 10 terakhir dari audit log, dengan filter “hanya saya”. | P1 |
| DASH-06 | Panel peringatan: cert expiry, publish gagal, backup gagal, disk > 85%, login mencurigakan. | P0 |
| DASH-07 | Quick actions: Buat situs, Publish config, Backup sekarang, Jalankan health check. | P1 |
| DASH-08 | Refresh otomatis 10 detik saat tab aktif; berhenti saat tab hidden; tanpa layout shift. | P1 |

### 7.3 Websites

**Tujuan:** siklus hidup situs lengkap tanpa menyentuh filesystem.

| ID | Requirement | Prioritas |
|---|---|---|
| SITE-01 | Daftar situs: nama, domain utama, tipe app, status, pemilik, ukuran disk, aksi cepat. | P0 |
| SITE-02 | Pencarian nama/domain, filter status/tipe/pemilik, sort, pagination 25/50/100. | P0 |
| SITE-03 | Wizard 3 langkah: (1) nama + domain, (2) tipe app + docroot + pemilik, (3) ringkasan + preview config. | P0 |
| SITE-04 | Tipe app v1.0: `static`, `php_fastcgi` (jika engine mendukung), `reverse_proxy` (v1.1). | P0 |
| SITE-05 | Validasi domain: FQDN RFC 1123, tanpa wildcard di v1, cek duplikat lintas situs, cek DNS A/AAAA dengan peringatan (bukan blocker). | P0 |
| SITE-06 | Docroot default `/var/www/<slug>/public_html`; hanya boleh di bawah `/var/www` (configurable global), tanpa `..`, tanpa symlink keluar. | P0 |
| SITE-07 | Pembuatan situs = satu transaksi: direktori, system user (opsional), config engine, metadata DB, reload. Gagal → rollback penuh. | P0 |
| SITE-08 | Hapus situs: ketik nama, opsi “simpan backup 7 hari”, opsi “hapus database terkait”. | P0 |
| SITE-09 | Nonaktifkan situs = 503 maintenance page, data tetap; aktifkan kembali tanpa rebuild config. | P1 |
| SITE-10 | Kloning situs (v1.1): salin file + config + DB opsional, domain wajib baru. | P2 |
| SITE-11 | Indikator drift: config di disk berbeda dari revisi terakhir yang dipublish panel. | P0 |
| SITE-12 | Bulk action: aktif/nonaktif, backup, hapus (dengan konfirmasi kolektif). | P2 |

**Acceptance:** membuat situs pada server bersih menghasilkan situs yang dapat diakses dalam ≤ 5 detik setelah submit; menghapus situs tidak menyisakan config engine; nama duplikat ditolak dengan 409.

### 7.4 Config Studio `.ginxaccess` (diferensiasi inti)

**Tujuan:** menjadikan config per direktori dapat diedit dengan percaya diri: tahu apa yang ditulis, apa efeknya, dan bagaimana membatalkannya.

#### 7.4.1 Model data dan alur

```text
Draft (SQLite) ──validate──> Valid/Invalid ──diff──> Publish
      ▲                         │                      │
      │                         ▼                      ▼
      └── edit ◄── engine error  Engine `-t`      atomic write + reload
                                                     │
                                                     ▼
                                             Published revision
                                                     │
                                          gagal sehat? ──> auto-rollback
```

| ID | Requirement | Prioritas |
|---|---|---|
| CFG-01 | Editor kode (CodeMirror 6) dengan sintaks `.ginxaccess`, nomor baris, bracket matching, dan autocomplete directive dari skema. | P0 |
| CFG-02 | Panel kiri: pohon direktori docroot yang memiliki `.ginxaccess`; status per file (valid, invalid, drift, belum ada). | P0 |
| CFG-03 | Validasi server-side memakai parser/evaluator engine yang sama (import `aginx/internal/ginxaccess`), bukan parser duplikat. | P0 |
| CFG-04 | Error ditampilkan `path:baris:kolom: pesan`; klik melompat ke baris; error tidak menghapus draft. | P0 |
| CFG-05 | Lint tambahan panel: directive tidak dikenal, duplikat berbahaya, regex > 1024 byte, file > 64 KiB, directive > 256, include di luar allowlist, nilai header mengandung CR/LF. | P0 |
| CFG-06 | Simulator: form method, path, IP, User-Agent, Referer, header kustom → hasil rantai config yang dievaluasi, aturan yang match, aksi akhir (allow/deny/rewrite/redirect/respond), status, dan header efektif. | P0 |
| CFG-07 | Visualisasi inheritance: pohon docroot → direktori target, directive per level, hasil merge, dan konflik (mis. child mencoba membuka `deny` parent). | P0 |
| CFG-08 | Diff sebelum publish (unified diff) + ringkasan dampak: jumlah rule berubah, path terdampak, direktif yang menambah/mengurangi akses. | P0 |
| CFG-09 | Publish: validasi ulang → tulis temp di direktori yang sama → `fsync` file → `rename` → `fsync` direktori → reload engine → health check. | P0 |
| CFG-10 | Revisi: setiap publish menyimpan konten, hash, author, waktu, hasil validasi; retensi 100 revisi atau 180 hari. | P0 |
| CFG-11 | Rollback satu klik ke revisi mana pun; rollback juga melalui validasi + publish pipeline dan dicatat di audit. | P0 |
| CFG-12 | Konflik konkuren: publish membawa `If-Match` hash revisi; jika berubah → 409 dengan diff. | P0 |
| CFG-13 | Mode aman: jika engine tidak sehat atau validasi gagal, tombol publish disabled dengan alasan. | P0 |
| CFG-14 | Template: WordPress-style, API protection, asset caching, admin lock, maintenance, SPA fallback. Template mengisi editor, tidak langsung publish. | P1 |
| CFG-15 | Snippet library: sisipkan blok di posisi kursor (mis. security headers, hotlink, rate limit). | P1 |
| CFG-16 | Form builder untuk 8 direktif umum menghasilkan snippet teks (bukan mengubah seluruh file). | P1 |
| CFG-17 | Pencarian global config: cari directive/value di seluruh situs, tampilkan file:baris. | P2 |
| CFG-18 | Format kanonik (opsional, eksplisit): merapikan indentasi dan urutan tanpa mengubah semantik; selalu menampilkan diff. | P2 |

#### 7.4.2 Aturan keamanan Config Studio

- Hanya directive pada whitelist engine v1; tidak ada `exec`, `include` absolut, atau path di luar docroot.
- Tidak ada evaluasi kode, template, atau environment variable.
- Ukuran file, jumlah rule, kedalaman block, dan panjang regex divalidasi **sebelum** menyentuh disk.
- `.ginxaccess` tidak boleh memuat kredensial; Basic Auth memakai `auth_user_file` di `/etc/aginx/auth/`.
- Konten config dianggap tidak tepercaya saat ditampilkan: escape HTML, render di `<pre>`, tidak pernah `innerHTML`.
- Publish selalu diaudit: actor, IP, site, path, hash lama/baru, hasil.

#### 7.4.3 Peta fitur panel → directive

| Fitur UI | Directive | Catatan |
|---|---|---|
| Rewrite | `rewrite "regex" "/target" last;` | Regex RE2, capture `${1}`; tampilkan preview hasil |
| Redirect | `redirect 301\|302\|307\|308 "regex" "target";` | Target divalidasi; host absolut harus ada di allowlist |
| IP ACL | `allow`, `deny`, `acl_order` | Tampilkan hasil merge parent; child tidak dapat membuka deny parent |
| Header | `header set/add/unset Nama "nilai";` | Blokir header framing/hop-by-hop |
| Basic Auth | `auth_basic`, `auth_user_file` | Kelola user file di UI terpisah |
| Custom error | `error_page 404 "/errors/404.html";` | Validasi file ada saat publish (warning jika tidak) |
| Cache-Control | `cache_control "mime" "value";`, `expires "mime" detik;` | Wildcard MIME didukung |
| UA block | `user_agent deny\|allow "regex";` | Peringatkan bahwa UA dapat dipalsukan |
| Rate limit | `rate_limit rps burst;` | Per IP; tampilkan estimasi memori tabel |
| Directory index | `directory_index a b;` | Nama file saja |
| Listing | `directory_listing off;` | Default off; child tidak dapat mengaktifkan jika global lock |
| Hotlink | `hotlink allow_empty on;`, `hotlink_allow "regex";` | Referer dapat dipalsukan; bukan auth |
| Maintenance | `respond 503;` + `error_page 503` | Sediakan toggle + jadwal |
| Conditional | `<If>`, `<Files>`, `<Match>` | Form kondisi terbatas pada grammar |

**Acceptance:** config yang ditolak engine menghasilkan pesan baris yang benar; publish yang sukses terbukti aktif lewat probe HTTP; rollback memulihkan byte-identik konten sebelumnya; simulasi untuk 20 skenario golden cocok dengan keputusan engine.

### 7.5 SSL/TLS

| ID | Requirement | Prioritas |
|---|---|---|
| SSL-01 | Inventaris sertifikat: domain, issuer, not_before/not_after, status, auto-renew, last renewal error. | P0 |
| SSL-02 | Terbitkan Let's Encrypt via HTTP-01; preflight: port 80 terbuka, DNS mengarah ke server, tidak ada redirect loop. | P0 |
| SSL-03 | Mode staging untuk pengujian; produksi hanya setelah sukses staging (opsional). | P1 |
| SSL-04 | Upload custom cert + key (validasi chain, key cocok, belum kedaluwarsa); key disimpan terenkripsi 0600. | P0 |
| SSL-05 | Auto-renew H-30, retry Harian H-14, alert H-14/H-7/H-1; tidak pernah menghapus cert lama sebelum yang baru aktif. | P0 |
| SSL-06 | Toggle “Force HTTPS” menghasilkan redirect 308 yang dikelola panel (bukan menimpa config pengguna). | P0 |
| SSL-07 | HSTS hanya dapat diaktifkan setelah HTTPS sehat; default `max-age=31536000; includeSubDomains` dapat diedit. | P1 |
| SSL-08 | SNI multi-domain per situs; sertifikat per domain (v1.1). | P1 |
| SSL-09 | Deteksi konflik port 80 dengan service lain sebelum ACME; tampilkan perbaikan. | P0 |
| SSL-10 | Revoke sertifikat dan bersihkan file terkait. | P2 |

### 7.6 File Manager

| ID | Requirement | Prioritas |
|---|---|---|
| FILE-01 | Scope hanya docroot situs; root filesystem tidak dapat dijelajahi dari UI. | P0 |
| FILE-02 | List/grid, breadcrumb, ukuran, mtime, permission, owner; sort dan filter. | P0 |
| FILE-03 | Upload drag-and-drop, multi-file, progress, resume untuk file > 100 MB, batas 2 GB (configurable). | P0 |
| FILE-04 | Editor teks ≤ 2 MB dengan deteksi binary; simpan atomik (temp + rename), backup `.bak` sekali per sesi. | P0 |
| FILE-05 | Buat folder/file, rename, move, copy, duplicate, delete ke trash (`.aginx-trash/`, retensi 7 hari). | P0 |
| FILE-06 | Arsip: extract zip/tar.gz/tar dengan proteksi zip-slip dan batas rasio dekompresi; create arsip. | P1 |
| FILE-07 | Permission/ownership: chmod terbatas (tidak setuid/setgid), chown hanya ke system user situs. | P1 |
| FILE-08 | Pencarian nama dan isi (≤ 10 MB/file, maks 100 hasil) dengan indeks on-demand. | P2 |
| FILE-09 | Kuota per situs dan peringatan disk; blokir upload jika melewati kuota. | P1 |
| FILE-10 | Semua operasi path memakai `openat2` dengan `RESOLVE_BENEATH \| RESOLVE_NO_SYMLINKS`; tolak device/FIFO/socket. | P0 |

### 7.7 Databases (MySQL/MariaDB)

| ID | Requirement | Prioritas |
|---|---|---|
| DB-01 | Deteksi engine dan versi; jika tidak terpasang, tampilkan panduan instalasi (tidak auto-install di v1). | P0 |
| DB-02 | List database: nama, ukuran, jumlah tabel, situs terkait, user. | P0 |
| DB-03 | Buat DB + user dengan grant hanya ke DB tersebut (`SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, INDEX, ALTER`). | P0 |
| DB-04 | Reset password user, revoke akses, hapus DB (konfirmasi nama). | P0 |
| DB-05 | Dump/restore via `mysqldump`/`mysql` dengan argumen array, tanpa shell; job + progress + checksum. | P1 |
| DB-06 | Tampilkan connection string siap salin; password hanya terlihat sekali atau dengan re-auth. | P0 |
| DB-07 | Guard: tolak nama `mysql`, `information_schema`, `performance_schema`, `sys`. | P0 |
| DB-08 | PostgreSQL (v1.1) dengan model yang sama. | P2 |

### 7.8 Cron

| ID | Requirement | Prioritas |
|---|---|---|
| CRON-01 | List job per situs/pengguna: jadwal, perintah, status, last run, next 5 runs. | P0 |
| CRON-02 | Create/edit 5-field cron atau preset (tiap menit/5 menit/jam/harian/mingguan/bulanan); validasi sintaks. | P0 |
| CRON-03 | Perintah dijalankan sebagai system user situs, working dir docroot, environment minimal. | P0 |
| CRON-04 | Enable/disable, run now (di luar jadwal), lihat output 100 baris terakhir, hapus. | P0 |
| CRON-05 | Batas 200 job/server; peringatan untuk jadwal < 1 menit; tidak ada edit crontab mentah. | P1 |
| CRON-06 | Semua perubahan dicatat di audit; output disimpan 7 hari. | P1 |

### 7.9 Terminal

| ID | Requirement | Prioritas |
|---|---|---|
| TERM-01 | Terminal xterm.js via WebSocket + PTY; resize, copy/paste, reconnect 1x. | P0 |
| TERM-02 | Tiket sesi sekali pakai, berlaku 30 detik, terikat user+IP. | P0 |
| TERM-03 | Shell default sebagai system user situs; shell root hanya untuk role owner/admin dengan re-auth dan peringatan. | P0 |
| TERM-04 | Idle timeout 15 menit, maksimum 3 sesi per user, tombol “putus semua”. | P0 |
| TERM-05 | Recording sesi (asciinema-compatible) untuk shell root, retensi 30 hari, dapat diunduh auditor. | P1 |
| TERM-06 | Dapat dinonaktifkan total lewat setting `security.terminal_enabled=false`. | P1 |
| TERM-07 | Tidak ada eksekusi perintah dari luar sesi terminal (RPC tidak menerima command string). | P0 |

### 7.10 Security dan Firewall

| ID | Requirement | Prioritas |
|---|---|---|
| SEC-01 | Deteksi backend firewall: `nftables` (prioritas), `ufw`, `firewalld`; satu backend aktif, tidak mencampur. | P0 |
| SEC-02 | CRUD rule: action, protocol, port/range, source CIDR, komentar, urutan, enable. | P0 |
| SEC-03 | Apply dengan **commit-confirm**: rule diterapkan, auto-revert 5 menit jika tidak dikonfirmasi; selalu pertahankan akses SSH/panel. | P0 |
| SEC-04 | Preset: SSH, HTTP, HTTPS, panel port, blokir semua kecuali allowlist. | P0 |
| SEC-05 | Login protection bawaan: lockout AUTH-05 + daftar IP gagal login + blokir manual. | P1 |
| SEC-06 | IP allowlist untuk panel (CIDR); jika diaktifkan, validasi bahwa IP saat ini termasuk. | P0 |
| SEC-07 | WAF preset yang dikompilasi ke `.ginxaccess`: blokir UA scanner, rate limit per path sensitif, tolak path traversal, blokir akses file sensitif (`.env`, `.git`, `*.sql`, `*.bak`). | P1 |
| SEC-08 | Security headers preset: HSTS, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, CSP dasar (opt-in). | P1 |
| SEC-09 | Deteksi port terbuka yang tidak diharapkan (database 0.0.0.0, Redis tanpa auth) dan tampilkan rekomendasi. | P1 |
| SEC-10 | Tampilkan status SELinux/AppArmor dan peringatan jika konteks file berpotensi memblokir serve. | P1 |
| SEC-11 | Tidak ada ModSecurity; WAF adalah kumpulan aturan AGinX, bukan engine eksternal. | — |

### 7.11 Logs

| ID | Requirement | Prioritas |
|---|---|---|
| LOG-01 | Sumber: access/error per situs, engine, panel, auth, job, systemd journal. | P0 |
| LOG-02 | Tail real-time via SSE, pause/resume, autoscroll, maks 500 baris di DOM (virtualized). | P0 |
| LOG-03 | Filter: waktu, status, method, path contains, IP, UA; kombinasi AND. | P0 |
| LOG-04 | Unduh rentang terpilih (maks 100 MB) dengan redaksi `Authorization`, `Cookie`, query sensitif. | P1 |
| LOG-05 | Rotasi: ukuran (default 100 MB), jumlah (default 7), kompresi; konfigurasi per sumber. | P1 |
| LOG-06 | Parser access log JSON bila engine mendukung; fallback combined log. | P0 |
| LOG-07 | Tampilkan request ID dan korelasi dengan audit event bila ada. | P2 |

### 7.12 Monitoring

| ID | Requirement | Prioritas |
|---|---|---|
| MON-01 | Sampling sistem 10 detik (CPU, RAM, swap, disk, inode, network, load, proses top 5). | P0 |
| MON-02 | Sampling engine via admin socket: RPS, koneksi, status class, cache hit/miss, latency p50/p95/p99 jika tersedia. | P0 |
| MON-03 | Per situs: RPS, bandwidth, 4xx/5xx, top path, top IP (dari access log terstruktur). | P1 |
| MON-04 | Retensi: ring buffer 24 jam resolusi 10 detik; rollup 5 menit 30 hari di SQLite. | P0 |
| MON-05 | Alert threshold: CPU > 90% 5 menit, RAM > 90%, disk > 85%, engine down, 5xx > 5%, cert < 14 hari. | P1 |
| MON-06 | Halaman proses: top 20 by CPU/RAM, kill hanya untuk proses milik situs dan dengan konfirmasi. | P2 |
| MON-07 | Endpoint `/metrics` panel (Prometheus) untuk self-observability, opt-in. | P2 |

### 7.13 Backup dan Restore

| ID | Requirement | Prioritas |
|---|---|---|
| BKP-01 | Backup manual/scheduled per situs: files, config, database terkait, metadata panel. | P0 |
| BKP-02 | Format `tar.gz` (streaming), manifest JSON (site, ukuran, checksum per file), checksum SHA-256 arsip. | P0 |
| BKP-03 | Destinasi v1.0: direktori lokal; v1.1: S3-compatible dan SFTP. | P0 |
| BKP-04 | Retensi: jumlah (default 7) dan/atau umur (default 30 hari); pruning hanya setelah backup baru terverifikasi. | P0 |
| BKP-05 | Restore: dry-run menampilkan file yang akan ditimpa, lalu restore ke direktori sementara → swap; otomatis backup sebelum restore. | P0 |
| BKP-06 | Enkripsi opsional (AES-256-GCM streaming) dengan passphrase yang tidak disimpan. | P2 |
| BKP-07 | Job progress, cancel, log, notifikasi sukses/gagal. | P0 |
| BKP-08 | Uji restore otomatis bulanan ke direktori sementara + verifikasi checksum. | P2 |

### 7.14 Users dan RBAC

| ID | Requirement | Prioritas |
|---|---|---|
| USR-01 | CRUD pengguna panel: username, email, nama, status, role. | P0 |
| USR-02 | Role: `owner` (semua), `admin` (semua kecuali hapus owner/ubah keamanan global), `operator` (situs yang di-assign), `viewer` (read-only). | P0 |
| USR-03 | Membership per situs dengan role `manager`/`viewer`; operator hanya melihat situs yang di-assign. | P0 |
| USR-04 | Paksa 2FA untuk `owner`/`admin`; reset 2FA oleh owner lain. | P0 |
| USR-05 | Tidak dapat menghapus diri sendiri atau owner terakhir. | P0 |
| USR-06 | Undangan via link sekali pakai 24 jam (tanpa email wajib); admin menyalin link. | P1 |
| USR-07 | Tabel permission terpusat, ditegakkan di agen (bukan hanya UI). | P0 |

### 7.15 Settings

| ID | Requirement | Prioritas |
|---|---|---|
| SET-01 | Umum: nama panel, URL publik, bahasa, timezone, format tanggal. | P0 |
| SET-02 | Engine: path binary, path config global, tombol Test (`aginx -t`), Reload, Restart, versi. | P0 |
| SET-03 | Keamanan: session TTL, kebijakan 2FA, IP allowlist, terminal on/off, batas login. | P0 |
| SET-04 | Notifikasi: SMTP host/port/user/pass/from, tes kirim; webhook URL + secret (v1.1). | P0 |
| SET-05 | Update: cek versi panel + engine, channel stable/beta, verifikasi signature, jadwal. | P1 |
| SET-06 | API token: buat/cabut, scope, last used. | P1 |
| SET-07 | Danger zone: reset data panel, uninstall, hapus semua situs (double confirm). | P0 |
| SET-08 | Ekspor/impor konfigurasi panel (tanpa secret) untuk migrasi. | P2 |

### 7.16 Notifications

| ID | Requirement | Prioritas |
|---|---|---|
| NOTIF-01 | In-app: bell + halaman daftar, mark read, filter level. | P0 |
| NOTIF-02 | Email via SMTP untuk event kritis (cert, backup gagal, engine down, brute force, publish gagal). | P1 |
| NOTIF-03 | Webhook generik (JSON POST + HMAC) v1.1. | P2 |
| NOTIF-04 | Deduplikasi 15 menit per (event, target); escalation jika berulang 3x. | P1 |
| NOTIF-05 | Preferensi per pengguna per kategori. | P1 |

### 7.17 Extensions (v2, batas desain)

- Manifest `extension.json`: nama, versi, kompatibilitas, izin (`sites:read`, `sites:write`, `metrics:read`), signature.
- UI slot: halaman, widget dashboard, aksi tab situs; frontend di-serve dari bundle ekstensi.
- Backend hanya dapat memanggil API panel dengan token ber-scope; **tidak ada** plugin native yang memuat `.so`.
- Instalasi wajib signature valid + konfirmasi izin. Katalog aplikasi (WordPress/Laravel) memakai mekanisme ini.

---

## 8. Integrasi dengan Engine AGinX

### 8.1 Kontrak yang harus tersedia di engine

Panel bergantung pada kemampuan engine berikut. Kolom “Status” merujuk POC saat ini (`docs/02-arsitektur.md`).

| Kapabilitas engine | Dibutuhkan panel | Status POC | Fallback sementara |
|---|---|---|---|
| Multi-site / `server` block | Daftar situs, domain, docroot | Belum ada | 1 situs per proses (dev only) |
| TLS/SNI + sertifikat | Modul SSL | Belum ada | Upload manual, engine v1 |
| `aginx -t <file>` | Validasi config | Ada (subset) | — |
| Reload graceful (SIGHUP/admin) | Publish config | Ada (cache invalidate) | — |
| Admin socket status/metrics | Dashboard, monitoring | Belum ada | Parse log + `ss` (degraded) |
| Access log terstruktur JSON | Logs, per-site metrics | Belum ada | Combined log parsing |
| FastCGI/proxy | Tipe app PHP/proxy | Belum ada | Static only di MVP |
| inotify watcher | Deteksi perubahan eksternal | Belum ada | TTL 100 ms + drift check |
| Master-worker | Isolasi & upgrade | Belum ada | systemd restart (downtime) |

**Keputusan:** panel tidak akan mem-parsing ulang sintaks engine secara mandiri. Validasi, simulasi, dan merge memakai paket engine yang di-import. Jika engine belum menyediakan API tertentu, panel menampilkan status **degraded** dan menyembunyikan aksi yang tidak aman.

### 8.2 Format config yang dikelola panel (usulan kontrak)

File global `/etc/aginx/aginx.conf`:

```nginx
worker_processes auto;
user aginx;
admin_socket /run/aginx/admin.sock;
access_log /var/log/aginx/access.log json;

http {
    include /etc/aginx/sites/*.conf;
}
```

File situs `/etc/aginx/sites/<slug>.conf`:

```nginx
server {
    listen 443 ssl;
    server_name example.com www.example.com;
    root /var/www/example.com/public_html;

    ssl_certificate     /etc/aginx/certs/example.com/fullchain.pem;
    ssl_certificate_key /etc/aginx/certs/example.com/privkey.pem;

    access_log /var/log/aginx/example.com.access.log json;
    error_log  /var/log/aginx/example.com.error.log;
}
```

Catatan:

- `.ginxaccess` **tidak** di-include manual; engine membacanya otomatis dari docroot ke target.
- Panel menulis file situs secara atomik dan tidak pernah menulis komentar “generated” yang menghalangi edit manual; perubahan manual terdeteksi sebagai drift.
- Sintaks global di atas adalah **usulan** dan harus dibekukan bersama engine sebelum implementasi §14 Milestone 2.

### 8.3 Alur publish yang wajib

1. Ambil draft + hash revisi terakhir.
2. Tulis ke file temp di direktori target (`.<name>.tmp.<rand>`), `chmod 0644`, `fsync`.
3. Jalankan validator engine (in-process atau `aginx -t`) pada file temp.
4. Jika invalid: hapus temp, kembalikan error baris/kolom, tidak menyentuh file aktif.
5. Jika valid: `rename` temp → target, `fsync` direktori.
6. Reload engine (SIGHUP atau admin `POST /reload`), tunggu readiness ≤ 5 detik.
7. Probe HTTP ke domain/situs (HEAD `/` dengan Host) dan bandingkan status yang diharapkan.
8. Jika probe gagal: pulihkan konten sebelumnya dari revisi, reload ulang, tandai publish gagal, notifikasi.
9. Simpan revisi published + audit.

### 8.4 Pemetaan data panel → filesystem

| Data | Lokasi | Permission |
|---|---|---|
| Docroot situs | `/var/www/<slug>/public_html` | `0750`, owner system user, group `aginx` |
| Config engine global | `/etc/aginx/aginx.conf` | `0644` root |
| Config situs | `/etc/aginx/sites/<slug>.conf` | `0644` root |
| `.ginxaccess` | di docroot | `0644`, owner system user |
| Auth user file | `/etc/aginx/auth/<id>.users` | `0600` root |
| Sertifikat | `/etc/aginx/certs/<domain>/` | key `0600`, cert `0644` |
| Log | `/var/log/aginx/` | `0750`, group `aginx` |
| Database panel | `/var/lib/aginx-panel/panel.db` | `0600` root |
| Secret key | `/etc/aginx-panel/secret.key` | `0400` root |
| Job/log panel | `/var/lib/aginx-panel/jobs/` | `0700` root |

---

## 9. Arsitektur Teknis

### 9.1 Diagram komponen

```text
                         HTTPS 8443 (default 127.0.0.1)
 ┌───────────────┐      ───────────────────────────────►  ┌──────────────────────────┐
 │ Browser SPA   │                                        │ aginx-panel (web)        │
 │ React + TS    │  ◄──── SSE / WebSocket ───────────────  │ user: aginx-panel        │
 └───────────────┘                                        │ - serves UI (go:embed)   │
                                                          │ - REST API v1            │
                                                          │ - auth gateway           │
                                                          └────────────┬─────────────┘
                                                                       │ unix socket
                                                       /run/aginx-panel/agent.sock
                                                                       │ typed RPC (JSON)
                                                          ┌────────────▼─────────────┐
                                                          │ aginx-paneld (agent)     │
                                                          │ user: root               │
                                                          │ - policy + audit         │
                                                          │ - SQLite (WAL)           │
                                                          │ - config writer          │
                                                          │ - jobs                   │
                                                          └───┬───────┬───────┬──────┘
                                                              │       │       │
                                              ┌───────────────┘       │       └────────────────┐
                                              ▼                       ▼                        ▼
                                     filesystem (openat2)      systemd / signals       AGinX admin socket
                                     /var/www, /etc/aginx      aginx, mysql, cron      /run/aginx/admin.sock
```

### 9.2 Proses dan privilege

| Proses | User | Tanggung jawab | Tidak boleh |
|---|---|---|---|
| `aginx-panel` (web) | `aginx-panel` | Serve UI, REST, sesi, validasi input awal, proxy RPC, SSE/WS | Menyentuh filesystem situs, menulis config, jadi root |
| `aginx-paneld` (agent) | `root` | Auth final, otorisasi, SQLite, tulis config, jalankan job, panggil systemd/engine, audit | Membuka port jaringan |
| `aginx` (engine) | `aginx` | Serve trafik | Menulis config panel |

Alasan pemisahan: kompromi pada parser HTTP/UI tidak otomatis memberi root. Trade-off: dua unit systemd, IPC, dan kompleksitas deployment. Mitigasi: satu binary dengan dua mode (`serve`, `agent`), instalasi membuat dua unit, health check gabungan.

**Isolasi per situs:** v1.0 membuat system user per situs untuk kepemilikan file dan eksekusi cron/terminal. Engine tetap berjalan sebagai satu user `aginx` karena POC belum mendukung worker per-situs; akibatnya isolasi runtime antar situs **belum** terjamin. Ini dicatat sebagai risiko R-06 dan alasan kuat untuk worker pool per-situs di engine v2.

### 9.3 Stack dan dependency

| Lapisan | Pilihan | Alasan | Trade-off |
|---|---|---|---|
| Backend | Go 1.24+, `net/http` (pattern routing 1.22+) | Sama dengan engine; satu bahasa; static binary | Tidak ada framework ORM/validasi; harus disiplin |
| Database | SQLite via `modernc.org/sqlite` (pure Go) | Transaksional, embedded, tanpa cgo/static | Binary +6–8 MB; write concurrency terbatas (mitigasi WAL + single writer) |
| Migrasi | File SQL berurutan + tabel `schema_migrations` | Sederhana, deterministik | Tidak ada down migration otomatis |
| Hash password | Argon2id (`golang.org/x/crypto/argon2`) | Rekomendasi OWASP | Dependency x/crypto |
| TOTP | Implementasi RFC 6238 dengan stdlib | Menghindari dependency tambahan | Wajib uji vektor RFC |
| ACME | `golang.org/x/crypto/acme` | Jangan implementasi crypto sendiri | Perlu uji staging |
| Frontend | React 19 + TypeScript 5 + Vite 7 | Ekosistem komponen, DX, bundle terkontrol | Toolchain Node saat build |
| Styling | Tailwind CSS 4 + CSS variables | Token konsisten, dark mode | Perlu disiplin kelas |
| Data fetching | TanStack Query | Cache, retry, invalidasi | Sedikit kompleksitas |
| Tabel | TanStack Table (headless) | Fleksibel, aksesibel | Harus bangun styling sendiri |
| Editor | CodeMirror 6 | Ringan,Lezer grammar, mode read-only | Perlu definisi grammar |
| Terminal | xterm.js | Standar de facto | Bundle tambahan |
| Chart | uPlot | Sangat ringan, cepat | API rendah-level |
| Test frontend | Vitest + Testing Library + Playwright | Unit + e2e | Waktu CI |
| Build UI | `go:embed` hasil `web/dist` | Satu binary | Build dua tahap |

**Kebijakan dependency:** daftar dependency backend maksimal 6 modul langsung. Setiap penambahan harus melalui keputusan tertulis (ADR) dan tidak boleh menambah kebutuhan runtime eksternal.

### 9.4 Struktur repositori

Panel berada dalam modul Go yang sama (`module aginx`) agar dapat meng-import parser engine. Struktur:

```text
control-panel/
  PRD.md
  Makefile
  go.mod
  cmd/aginx-panel/
    main.go                     # flag, mode serve/agent, lifecycle
  internal/
    api/
      router.go                 # route table /api/v1
      middleware.go             # request id, auth, csrf, rate limit, audit
      handlers/                 # satu file per resource
      dto/                      # request/response types + validasi
    auth/
      password.go               # argon2id
      totp.go                   # RFC 6238
      session.go                # cookie, rotasi, revocation
      rbac.go                   # permission matrix
    agent/
      client.go                 # typed client untuk web
      server.go                 # unix socket server
      rpc.go                    # method registry + schema
      policy.go                 # allowlist, peer cred, otorisasi
    store/
      db.go                     # open, pragma, tx helper
      migrations/               # *.sql
      users.go, sites.go, ...
    sites/
      service.go                # lifecycle situs
      configgen.go              # render server block
      provision.go              # user, dir, permission
    ginxstudio/
      lint.go                   # aturan tambahan panel
      simulate.go               # memakai evaluator engine
      diff.go
      publish.go                # atomic write + reload + probe
    files/
      fsops.go                  # openat2, upload, archive
      trash.go
    ssl/
      acme.go
      store.go
    monitor/
      proc.go                   # /proc sampling
      ring.go                   # ring buffer + rollup
    logs/
      tail.go
      parse.go
    jobs/
      queue.go
      runner.go
    backup/
      archive.go
      restore.go
    firewall/
      nft.go, ufw.go, firewalld.go
    cron/
    terminal/
    notify/
    audit/
    version/
  web/
    package.json
    vite.config.ts
    index.html
    src/
      main.tsx
      app/router.tsx
      app/shell/               # sidebar, topbar, command palette
      features/                # dashboard, sites, config, ssl, files, ...
      components/              # button, table, drawer, form, empty, toast
      lib/api.ts               # client + error mapping
      lib/i18n/
      styles/tokens.css
  packaging/
    systemd/aginx-panel.service
    systemd/aginx-paneld.service
    deb/, rpm/
    tmpfiles.d/
    logrotate.d/
  tests/
    e2e/
    load/
    security/
  docs/
```

### 9.5 Model data (SQLite)

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE users (
  id             TEXT PRIMARY KEY,
  username       TEXT NOT NULL UNIQUE,
  email          TEXT NOT NULL UNIQUE,
  display_name   TEXT NOT NULL,
  password_hash  TEXT NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('owner','admin','operator','viewer')),
  totp_secret    BLOB,
  totp_enabled   INTEGER NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
  failed_count   INTEGER NOT NULL DEFAULT 0,
  locked_until   INTEGER,
  created_at     INTEGER NOT NULL,
  last_login_at  INTEGER
);

CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash    BLOB NOT NULL UNIQUE,
  csrf_hash     BLOB NOT NULL,
  ip            TEXT NOT NULL,
  user_agent    TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,
  revoked_at    INTEGER
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   BLOB NOT NULL UNIQUE,
  scopes       TEXT NOT NULL,          -- JSON array
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER,
  expires_at   INTEGER
);

CREATE TABLE sites (
  id             TEXT PRIMARY KEY,
  slug           TEXT NOT NULL UNIQUE,
  name           TEXT NOT NULL,
  primary_domain TEXT NOT NULL UNIQUE,
  docroot        TEXT NOT NULL,
  app_type       TEXT NOT NULL DEFAULT 'static'
                 CHECK (app_type IN ('static','php_fastcgi','reverse_proxy')),
  upstream       TEXT,
  system_user    TEXT,
  owner_id       TEXT REFERENCES users(id),
  status         TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active','disabled','error')),
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
);

CREATE TABLE domains (
  id         TEXT PRIMARY KEY,
  site_id    TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  fqdn       TEXT NOT NULL UNIQUE,
  kind       TEXT NOT NULL DEFAULT 'primary' CHECK (kind IN ('primary','alias','redirect')),
  redirect_to TEXT,
  created_at INTEGER NOT NULL
);

CREATE TABLE config_revisions (
  id            TEXT PRIMARY KEY,
  site_id       TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  rel_path      TEXT NOT NULL,           -- relatif docroot, mis. ".ginxaccess"
  content       TEXT NOT NULL,
  content_hash  TEXT NOT NULL,           -- sha256 hex
  status        TEXT NOT NULL CHECK (status IN ('draft','valid','invalid','published','superseded')),
  engine_errors TEXT,                    -- JSON
  author_id     TEXT REFERENCES users(id),
  created_at    INTEGER NOT NULL,
  published_at  INTEGER
);
CREATE INDEX idx_rev_site_path ON config_revisions(site_id, rel_path, created_at DESC);

CREATE TABLE published_configs (
  site_id      TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  rel_path     TEXT NOT NULL,
  revision_id  TEXT NOT NULL REFERENCES config_revisions(id),
  checksum     TEXT NOT NULL,
  published_at INTEGER NOT NULL,
  PRIMARY KEY (site_id, rel_path)
);

CREATE TABLE ssl_certificates (
  id          TEXT PRIMARY KEY,
  site_id     TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  domains     TEXT NOT NULL,            -- JSON array
  issuer      TEXT NOT NULL,
  not_before  INTEGER NOT NULL,
  not_after   INTEGER NOT NULL,
  cert_path   TEXT NOT NULL,
  key_path    TEXT NOT NULL,
  auto_renew  INTEGER NOT NULL DEFAULT 1,
  status      TEXT NOT NULL,
  last_error  TEXT,
  created_at  INTEGER NOT NULL
);

CREATE TABLE databases (
  id           TEXT PRIMARY KEY,
  engine       TEXT NOT NULL DEFAULT 'mysql',
  name         TEXT NOT NULL,
  db_user      TEXT NOT NULL,
  password_enc BLOB NOT NULL,
  site_id      TEXT REFERENCES sites(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  UNIQUE (engine, name)
);

CREATE TABLE cron_jobs (
  id          TEXT PRIMARY KEY,
  site_id     TEXT REFERENCES sites(id) ON DELETE CASCADE,
  system_user TEXT NOT NULL,
  schedule    TEXT NOT NULL,
  command     TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  last_run_at INTEGER,
  last_status TEXT,
  created_at  INTEGER NOT NULL
);

CREATE TABLE jobs (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL,            -- JSON
  status      TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','canceled')),
  progress    INTEGER NOT NULL DEFAULT 0,
  message     TEXT,
  created_by  TEXT REFERENCES users(id),
  created_at  INTEGER NOT NULL,
  started_at  INTEGER,
  finished_at INTEGER
);
CREATE INDEX idx_jobs_status ON jobs(status, created_at);

CREATE TABLE backups (
  id           TEXT PRIMARY KEY,
  site_id      TEXT REFERENCES sites(id) ON DELETE SET NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('manual','scheduled','pre_restore')),
  destination  TEXT NOT NULL,
  path         TEXT NOT NULL,
  size_bytes   INTEGER,
  sha256       TEXT,
  status       TEXT NOT NULL,
  started_at   INTEGER NOT NULL,
  finished_at  INTEGER
);

CREATE TABLE firewall_rules (
  id         TEXT PRIMARY KEY,
  action     TEXT NOT NULL CHECK (action IN ('allow','deny')),
  direction  TEXT NOT NULL DEFAULT 'in',
  protocol   TEXT NOT NULL CHECK (protocol IN ('tcp','udp','any')),
  port       TEXT NOT NULL,
  source     TEXT NOT NULL DEFAULT 'any',
  comment    TEXT,
  enabled    INTEGER NOT NULL DEFAULT 1,
  priority   INTEGER NOT NULL DEFAULT 100
);

CREATE TABLE audit_events (
  id          TEXT PRIMARY KEY,
  ts          INTEGER NOT NULL,
  actor_id    TEXT,
  actor_ip    TEXT,
  action      TEXT NOT NULL,
  target_type TEXT,
  target_id   TEXT,
  result      TEXT NOT NULL,
  meta        TEXT,                      -- JSON, sudah diredaksi
  prev_hash   TEXT,
  hash        TEXT NOT NULL
);
CREATE INDEX idx_audit_ts ON audit_events(ts DESC);

CREATE TABLE site_members (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role    TEXT NOT NULL CHECK (role IN ('manager','viewer')),
  PRIMARY KEY (site_id, user_id)
);

CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,             -- JSON
  updated_at INTEGER NOT NULL
);

CREATE TABLE notifications (
  id         TEXT PRIMARY KEY,
  level      TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  read_at    INTEGER,
  created_at INTEGER NOT NULL
);
```

Aturan penyimpanan secret: nilai sensitif (TOTP secret, password DB, key material) dienkripsi AES-256-GCM dengan key `/etc/aginx-panel/secret.key`. Nonce disimpan bersama ciphertext. Key tidak pernah dikirim ke web process.

### 9.6 Job queue

- Tabel `jobs` adalah antrean; agen menjalankan maksimal **2 job berat** paralel (backup/restore/dump) dan **4 job ringan** (probe, reload, cert issue).
- Setiap job punya `context` dengan cancel; cancel menandai `canceled` dan menghentikan proses anak (SIGTERM, lalu SIGKILL setelah 10 detik).
- Progress 0–100 + pesan; log job ke `/var/lib/aginx-panel/jobs/<id>.log`.
- Idempoten berdasarkan `(type, payload-hash)` selama 60 detik untuk mencegah double-submit.
- Retry: maksimal 2, hanya untuk error transien (network ACME, lock DB), bukan validasi.

### 9.7 Realtime

- **SSE** `GET /api/v1/events` untuk notifikasi, status job, dan status engine; heartbeat 20 detik; reconnect dengan `Last-Event-ID`.
- **SSE** `GET /api/v1/sites/{id}/logs/tail` untuk log.
- **WebSocket** `GET /api/v1/terminal?ticket=...` untuk PTY.
- Fallback polling 10 detik jika SSE gagal 3x.

### 9.8 Build dan packaging

- `make build` → build UI (`npm ci && npm run build`) → `go build -trimpath -ldflags "-s -w -X ...version"` → `bin/aginx-panel`.
- `make check` → `gofmt -l`, `go vet`, `go test -race ./...`, `npm run lint`, `npm run typecheck`, `npm test`.
- `.deb`/`.rpm` berisi binary, dua unit systemd, `tmpfiles.d`, `logrotate`, user sistem, direktori. Post-install membuat user, mengaktifkan unit agent lebih dulu, lalu web.
- Upgrade: hentikan web → migrasi DB (backup otomatis) → ganti binary → start web. Downtime target < 5 detik.
- Uninstall: pertahankan `/var/lib/aginx-panel` dan `/etc/aginx-panel` kecuali `--purge`.

---

## 10. API Specification

### 10.1 Konvensi

- Base path `/api/v1`; JSON UTF-8; field `snake_case`.
- ID resource: ULID string.
- Autentikasi: cookie sesi + `X-CSRF-Token`, atau `Authorization: Bearer <token>` untuk API token.
- Sukses: `{ "data": ..., "meta": { ... } }`. List: `meta: { page, per_page, total }`.
- Error: `{ "error": { "code": "site_not_found", "message": "...", "details": [...], "request_id": "..." } }`.
- Status: 200, 201, 202 (job), 204, 400, 401, 403, 404, 409, 422, 429, 500, 503.
- Pagination: `?page=1&per_page=25` (max 100). Sort: `?sort=-created_at`.
- Filter: query params eksplisit, bukan bahasa query bebas.
- Idempotency: header `Idempotency-Key` untuk POST create/backup/publish.
- Concurrency: `If-Match` berisi `content_hash` untuk publish.
- Rate limit: 120 req/menit per sesi; 10 login/menit per IP; header `X-RateLimit-*`.
- Semua response menyertakan `X-Request-ID`.
- Versi: perubahan breaking → `/api/v2`; penambahan field tidak breaking.

### 10.2 Endpoint

#### Auth dan profil

| Method | Path | Keterangan |
|---|---|---|
| GET | `/health` | liveness/readiness, tanpa auth |
| GET | `/setup/status` | apakah setup diperlukan |
| POST | `/setup` | buat owner pertama (token wajib) |
| POST | `/auth/login` | login + TOTP |
| POST | `/auth/logout` | cabut sesi |
| GET | `/auth/me` | profil + permission |
| POST | `/auth/totp/setup` | secret + QR (base32) |
| POST | `/auth/totp/verify` | aktifkan 2FA |
| POST | `/auth/totp/disable` | nonaktifkan (re-auth) |
| POST | `/auth/recovery-codes` | regenerasi |
| GET | `/auth/sessions` | daftar sesi |
| DELETE | `/auth/sessions/{id}` | cabut sesi |

#### Situs dan domain

| Method | Path | Keterangan |
|---|---|---|
| GET | `/sites` | list + filter |
| POST | `/sites` | buat situs (job sinkron ≤ 5 dtk) |
| GET | `/sites/{id}` | detail |
| PATCH | `/sites/{id}` | ubah nama/owner |
| DELETE | `/sites/{id}` | hapus (body: `keep_backup`, `drop_database`) |
| POST | `/sites/{id}/disable` | maintenance 503 |
| POST | `/sites/{id}/enable` | aktifkan |
| GET | `/sites/{id}/health` | probe HTTP + config + engine |
| GET | `/sites/{id}/domains` | list domain |
| POST | `/sites/{id}/domains` | tambah domain |
| PATCH | `/sites/{id}/domains/{domainId}` | ubah redirect |
| DELETE | `/sites/{id}/domains/{domainId}` | hapus domain |
| GET | `/sites/{id}/metrics?range=1h` | metrik situs |

#### Config Studio

| Method | Path | Keterangan |
|---|---|---|
| GET | `/sites/{id}/configs` | pohon file config + status |
| GET | `/sites/{id}/configs/content?path=.ginxaccess` | isi + hash + revisi |
| PUT | `/sites/{id}/configs/content` | simpan draft (bukan publish) |
| POST | `/sites/{id}/configs/validate` | validasi engine + lint panel |
| POST | `/sites/{id}/configs/simulate` | simulasi request |
| POST | `/sites/{id}/configs/publish` | publish (If-Match) |
| POST | `/sites/{id}/configs/rollback` | rollback ke revisi |
| GET | `/sites/{id}/configs/revisions` | riwayat |
| GET | `/sites/{id}/configs/revisions/{revId}/diff` | diff |
| GET | `/sites/{id}/configs/inheritance?path=/` | pohon + merge |
| GET | `/configs/health` | ringkasan semua config |

Contoh validasi gagal:

```json
{
  "error": {
    "code": "config_invalid",
    "message": "Konfigurasi ditolak engine",
    "details": [
      { "file": "admin/.ginxaccess", "line": 7, "column": 1,
        "message": "allow: CIDR tidak valid", "severity": "error" }
    ],
    "request_id": "01J9Z8K3V6T0YQ4M2R7N5B1C8D"
  }
}
```

Contoh publish:

```http
POST /api/v1/sites/01J9.../configs/publish
If-Match: "sha256:5d41402abc4b2a76b9719d911017c592..."
Content-Type: application/json

{ "path": ".ginxaccess", "revision_id": "01J9Z9..." }
```

```json
{
  "data": {
    "published": true,
    "revision_id": "01J9Z9...",
    "activated_at": "2026-11-05T14:32:11+07:00",
    "probe": { "status": 200, "latency_ms": 12 }
  }
}
```

#### SSL

| Method | Path | Keterangan |
|---|---|---|
| GET | `/ssl` | inventaris |
| POST | `/sites/{id}/ssl/issue` | ACME HTTP-01 (job) |
| POST | `/sites/{id}/ssl/upload` | cert + key |
| POST | `/sites/{id}/ssl/renew` | perpanjang manual |
| DELETE | `/sites/{id}/ssl/{certId}` | hapus/revoke |
| POST | `/sites/{id}/ssl/force-https` | toggle redirect |

#### Files

| Method | Path | Keterangan |
|---|---|---|
| GET | `/sites/{id}/files?path=/` | list direktori |
| GET | `/sites/{id}/files/download?path=...` | stream unduh |
| POST | `/sites/{id}/files/upload?path=/&name=...` | upload (multipart/chunk) |
| POST | `/sites/{id}/files/mkdir` | buat folder |
| POST | `/sites/{id}/files/rename` | rename/move |
| POST | `/sites/{id}/files/copy` | copy |
| DELETE | `/sites/{id}/files` | hapus ke trash |
| GET | `/sites/{id}/files/content?path=...` | baca teks |
| PUT | `/sites/{id}/files/content` | simpan teks |
| POST | `/sites/{id}/files/archive` | extract/create |
| POST | `/sites/{id}/files/chmod` | permission |

#### Database, cron, backup

| Method | Path | Keterangan |
|---|---|---|
| GET | `/databases` | list |
| POST | `/databases` | buat DB + user |
| DELETE | `/databases/{id}` | hapus |
| POST | `/databases/{id}/password` | reset password |
| POST | `/databases/{id}/dump` | job dump |
| GET | `/cron` | list |
| POST | `/cron` | buat |
| PATCH | `/cron/{id}` | ubah |
| DELETE | `/cron/{id}` | hapus |
| POST | `/cron/{id}/run` | run now |
| GET | `/backups` | list |
| POST | `/backups` | buat backup (job) |
| POST | `/backups/{id}/restore` | restore (job, dry-run flag) |
| DELETE | `/backups/{id}` | hapus arsip |

#### Sistem, keamanan, monitoring, pengguna

| Method | Path | Keterangan |
|---|---|---|
| GET | `/system/overview` | CPU/RAM/disk/load |
| GET | `/system/processes` | top proses |
| GET | `/engine/status` | status + versi + metrics |
| POST | `/engine/test` | `aginx -t` global |
| POST | `/engine/reload` | graceful reload |
| GET | `/firewall` | backend + rules |
| POST | `/firewall/rules` | tambah |
| PATCH | `/firewall/rules/{id}` | ubah |
| DELETE | `/firewall/rules/{id}` | hapus |
| POST | `/firewall/apply` | apply + commit token |
| POST | `/firewall/confirm` | konfirmasi (batalkan auto-revert) |
| GET | `/security/overview` | postur keamanan |
| GET | `/logs/sources` | daftar sumber |
| GET | `/logs/tail?...` | SSE tail |
| GET | `/logs/download?...` | unduh |
| GET | `/monitor/series?range=24h&metric=cpu` | time series |
| GET | `/users` | list |
| POST | `/users` | buat |
| PATCH | `/users/{id}` | ubah |
| DELETE | `/users/{id}` | hapus |
| GET | `/audit` | audit log |
| GET | `/settings` | semua setting (secret diredaksi) |
| PATCH | `/settings` | ubah |
| GET | `/jobs` | list job |
| GET | `/jobs/{id}` | detail + log |
| POST | `/jobs/{id}/cancel` | cancel |
| GET | `/notifications` | list |
| POST | `/notifications/read` | mark read |
| POST | `/terminal/ticket` | tiket WS sekali pakai |
| GET | `/events` | SSE global |

### 10.3 Kontrak agen (internal)

RPC JSON di unix socket, frame `{ "id", "method", "params", "context" }`. Metode **allowlist**:

```text
auth.login, auth.session.validate, auth.session.revoke
user.list, user.create, user.update, user.delete
site.list, site.get, site.create, site.update, site.delete, site.disable, site.enable
config.read, config.write_draft, config.validate, config.publish, config.rollback, config.simulate
ssl.list, ssl.issue, ssl.upload, ssl.renew, ssl.delete
files.list, files.stat, files.read, files.write, files.upload, files.delete,
files.mkdir, files.rename, files.copy, files.chmod, files.archive
db.list, db.create, db.delete, db.reset_password, db.dump
cron.list, cron.create, cron.update, cron.delete, cron.run
backup.list, backup.create, backup.restore, backup.delete
firewall.list, firewall.add, firewall.update, firewall.delete, firewall.apply
monitor.snapshot, monitor.series
logs.tail, logs.download
job.list, job.get, job.cancel
terminal.open
notification.list, notification.read
audit.list
settings.get, settings.update
engine.status, engine.test, engine.reload
```

Aturan:

- Agen memvalidasi sesi/token sendiri; klaim dari web tidak dipercaya.
- Setiap metode memvalidasi skema parameter (tipe, panjang, enum) sebelum eksekusi.
- Tidak ada metode `exec`, `shell`, atau path arbitrer. `terminal.open` membuat PTY secara eksplisit dan tercatat.
- `SO_PEERCRED` memastikan hanya UID `aginx-panel` yang dapat terhubung.
- Setiap panggilan menulis audit untuk metode mutasi.

---

## 11. Keamanan

### 11.1 Threat model

| Ancaman | Vektor | Kontrol |
|---|---|---|
| Credential stuffing | Login panel | Argon2id, lockout, 2FA wajib admin, notifikasi IP baru |
| Session hijack | Cookie | HttpOnly/Secure/SameSite, rotasi, idle timeout, binding IP opsional |
| CSRF | Form/aksi | Token per sesi + header, SameSite=Strict |
| XSS | Config, log, nama situs | React escaping, CSP, tanpa `innerHTML`, sanitasi nama file |
| Path traversal | File manager, config path | `openat2` + `RESOLVE_BENEATH`, tolak `..`, symlink, device |
| Command injection | Cron, DB, backup | `exec.Command` argumen array, tanpa shell, allowlist binary |
| SSRF | Reverse proxy upstream, webhook | Validasi skema/host, blokir loopback/link-local kecuali diizinkan |
| Privilege escalation | RPC agen | Allowlist metode, validasi sesi di agen, peer UID, tanpa shell |
| Config injection | `.ginxaccess` | Whitelist directive, batas ukuran, validasi engine, escape saat render |
| Symlink escape | File ops | `RESOLVE_NO_SYMLINKS`, cek device/inode |
| Backup exfiltration | Backup path | Path di bawah root backup, permission 0700, enkripsi opsional |
| Supply chain | Dependency UI/Go | Lockfile, `go.sum`, Renovate, review manual, build reproducible |
| Firewall lockout | Rule apply | Commit-confirm 5 menit, selalu izinkan SSH aktif |
| Terminal abuse | Root shell | Role gate, re-auth, recording, idle timeout, dapat dimatikan |
| Log injection | User-Agent/path | Escape saat render, JSON log, batas panjang field |

### 11.2 Header keamanan panel

```text
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline';
  img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'
Strict-Transport-Security: max-age=31536000; includeSubDomains   (hanya jika HTTPS panel aktif)
X-Content-Type-Options: nosniff
Referrer-Policy: no-referrer
X-Frame-Options: DENY
Permissions-Policy: camera=(), microphone=(), geolocation=()
Cache-Control: no-store   (respons API)
```

### 11.3 Audit log

- Aksi yang dicatat: login/logout/gagal login, semua mutasi situs/config/SSL/file/db/cron/firewall/user/setting, publish, rollback, restore, terminal open/close, perubahan izin.
- Field: actor, IP, user agent, action, target, result, metadata diredaksi (tanpa password/token/cookie), timestamp, request ID.
- Integritas: hash berantai `hash = SHA256(prev_hash || canonical(event))`; verifikasi tersedia via CLI.
- Retensi 180 hari; ekspor CSV/JSON oleh owner.
- Audit tidak dapat dihapus dari UI.

### 11.4 Default aman

- Panel listen `127.0.0.1:8443` + TLS self-signed saat instalasi; akses publik harus diaktifkan eksplisit.
- 2FA wajib untuk owner/admin.
- Tidak ada user default; setup token wajib.
- Terminal root nonaktif sampai diaktifkan di Settings.
- Direktori docroot `0750`, bukan `0777`.
- Firewall tidak mengubah aturan yang ada tanpa konfirmasi; backup aturan sebelum apply.
- Database tidak diekspos ke `0.0.0.0` oleh panel.

### 11.5 Update aman

- Manifest update ditandatangani (Ed25519); panel memverifikasi signature + SHA-256 sebelum mengganti binary.
- Update panel tidak pernah menimpa config engine atau docroot.
- Migrasi DB membuat backup otomatis dan berjalan dalam transaksi; gagal → rollback binary.

---

## 12. Non-Functional Requirements

| Kategori | Requirement |
|---|---|
| Performa API | p95 ≤ 150 ms (localhost, 100 situs); list 10.000 file ≤ 500 ms; dashboard aggregate ≤ 300 ms |
| Performa UI | Bundle awal ≤ 350 KB gzip; FCP ≤ 1,2 dtk LAN; tidak ada layout shift > 0,1 CLS |
| Resource | Web RSS ≤ 80 MB; agen ≤ 60 MB idle; CPU idle ≤ 1%; DB ≤ 50 MB untuk 100 situs |
| Skala v1 | 500 situs, 5.000 domain, 20.000 revisi config, 1 juta audit event (dengan retensi) |
| Ketersediaan | 99,9% panel; engine tidak terpengaruh restart panel |
| Durabilitas | SQLite WAL, `synchronous=NORMAL`, backup harian DB panel, config atomik |
| Recovery | RTO panel ≤ 2 menit; RPO metadata ≤ 5 menit (backup DB) |
| Kompatibilitas | 4 distro target, kernel ≥ 4.18, amd64/arm64 |
| Browser | Chrome/Edge/Firefox/Safari 2 versi terakhir |
| Aksesibilitas | WCAG 2.1 AA |
| Internasionalisasi | id-ID, en-US; semua string via katalog |
| Observability | Log JSON, request ID, `/health`, `/metrics` opt-in |
| Maintainability | Coverage backend ≥ 60% (paket kritis ≥ 80%), tanpa `go vet` warning |
| Keamanan | 0 critical/high pada audit; dependency scan bersih |

---

## 13. UX Copy dan Error

- Nada: ringkas, netral, teknis, tanpa menyalahkan pengguna.
- Contoh:
  - `Config ditolak pada baris 7: CIDR "10.0.0.0/33" tidak valid. Versi aktif tidak berubah.`
  - `Situs dibuat, tetapi sertifikat belum terbit: port 80 tidak dapat dijangkau dari internet.`
  - `Firewall diterapkan. Konfirmasi dalam 4:58 atau perubahan dibatalkan otomatis.`
  - `Backup selesai (1,2 GB, sha256 9f2c…). Restore membutuhkan 1,4 GB ruang kosong.`
- Hindari: “Oops”, “Something went wrong”, “Error!” tanpa konteks.
- Setiap error memiliki kode mesin (`config_invalid`, `engine_unreachable`, `firewall_lockout_risk`) dan request ID.

---

## 14. Roadmap dan Milestone

Estimasi untuk 1 backend senior + 1 frontend, full-time. Durasi dalam minggu kerja.

| Milestone | Durasi | Deliverable | Exit criteria |
|---|---|---|---|
| **M0 — Fondasi** | 1 | Repo panel, Makefile, CI, ADR stack, skeleton dua proses, health check | `make check` hijau; binary serve UI kosong + agen socket |
| **M1 — Auth + Shell** | 2 | Setup, login, TOTP, sesi, RBAC, AppShell, sidebar/topbar, command palette, design token | Login end-to-end; permission matrix tertest; a11y dasar lulus |
| **M2 — Situs + Config Studio** | 3 | CRUD situs, wizard, provision, editor, validasi engine, publish atomik, revisi, rollback, simulator dasar | Buat situs + publish config + rollback tanpa downtime di VM uji |
| **M3 — SSL + Files + Logs** | 2 | ACME HTTP-01, upload cert, file manager, tail log SSE | Situs HTTPS aktif dari UI; upload 1 GB; log real-time |
| **M4 — Security + Monitor + Backup** | 3 | Firewall adapter + commit-confirm, security overview, metrik sistem/engine, backup lokal + restore | Firewall tidak mengunci akses; restore terverifikasi checksum |
| **M5 — DB + Cron + Terminal + Users** | 3 | MySQL lifecycle, cron, terminal PTY, manajemen pengguna penuh | Semua modul lulus e2e; terminal role-gated |
| **M6 — Hardening + Packaging** | 2 | Audit keamanan, fuzz, packaging deb/rpm, systemd, docs, upgrade test | Instalasi bersih di 4 distro; audit tanpa critical/high |
| **v1.1** | 3–4 | Reverse proxy UI, PostgreSQL, S3/SFTP backup, alerting, webhook | — |
| **v2.0** | 6+ | Extensions, app catalog, multi-node | — |

**MVP (v0.9) = M0–M3**, dapat didemokan dengan engine mock bila engine v1 belum siap. **v1.0 = M0–M6.**

Ketergantungan eksternal: M2 dan M3 memblokir jika engine belum memiliki multi-site + TLS. Mitigasi: `internal/engine` adapter dengan dua implementasi (`real` dan `fake`) dan kontrak yang dibekukan lebih dulu.

---

## 15. Strategi Pengujian

| Jenis | Cakupan | Alat | Gate |
|---|---|---|---|
| Unit | RBAC, password/TOTP, config lint, path validation, cron parser, firewall render | `go test` | Coverage ≥ 60%, kritis ≥ 80% |
| Contract | RPC agen vs client, skema DTO | `go test` + golden JSON | Semua metode punya test |
| Integration | Situs → config → reload → probe, menggunakan binary AGinX asli di temp root | `go test -tags integration` | Wajib di CI nightly |
| E2E | Alur login, buat situs, publish, rollback, upload, backup/restore | Playwright | Blokir rilis |
| Fuzz | Parser config (sudah ada di engine), path file manager, RPC decoder, cron | `go test -fuzz` | 10 menit per target di CI nightly |
| Security | Path traversal, symlink, CSRF, XSS, IDOR, command injection, rate limit | Test suite + OWASP ZAP baseline | 0 high/critical |
| Load | 200 VU API, 50 publish paralel, 10k file listing | k6 | p95 sesuai §12 |
| Soak | 24 jam dengan trafik panel + job backup berulang | k6 + monitor | Tidak ada kebocoran memori/FD |
| Chaos | Kill -9 agen, disk penuh, engine mati, socket putus, DB lock | Script | Panel pulih tanpa kehilangan config |
| Upgrade | v1.0 → v1.1 dengan data nyata | VM snapshot | Migrasi < 60 dtk, rollback berhasil |
| Cross-distro | Debian 12/13, Ubuntu 22.04/24.04, AlmaLinux 9 | Vagrant/Lima | Instalasi + e2e smoke lulus |

**Definition of Done per fitur:** requirement terpenuhi; unit + e2e; audit event; error state; empty state; loading state; string i18n; dokumentasi pengguna; tidak ada TODO di kode produksi.

---

## 16. Dokumentasi yang Harus Dihasilkan

1. `README.md` panel: instalasi, arsitektur singkat, tautan.
2. `docs/admin-guide.md`: setup, TLS, backup, upgrade, troubleshooting.
3. `docs/user-guide.md`: situs, config, SSL, file, database, cron.
4. `docs/api.md`: referensi REST + contoh cURL.
5. `docs/security.md`: threat model, hardening, audit.
6. `docs/runbook.md`: insiden (engine down, disk penuh, cert gagal, firewall lockout).
7. `docs/adr/`: keputusan arsitektur (stack, SQLite, privilege split, config publish).

---

## 17. Risiko dan Mitigasi

| ID | Risiko | Dampak | Prob. | Mitigasi |
|---|---|---|---|---|
| R-01 | Engine v1 (multi-site/TLS/metrics) belum siap | Blokir M2–M4 | Tinggi | Kontrak dibekukan; adapter + fake engine; milestone gating |
| R-02 | Agen root menjadi jalur RCE | Kritis | Sedang | Allowlist RPC, tanpa shell, validasi skema, peer UID, audit, fuzz |
| R-03 | Scope creep ke panel hosting penuh (mail/DNS/Docker) | Jadwal | Tinggi | Non-goals §4.2; perubahan scope via PRD baru |
| R-04 | Semantik panel berbeda dari engine | Kepercayaan pengguna | Sedang | Import parser/evaluator engine; golden parity test |
| R-05 | SQLite write contention | UI lambat | Sedang | WAL, single writer, antrean job, indeks, ukur |
| R-06 | Isolasi runtime antar situs lemah (satu user engine) | Keamanan | Sedang | Dokumentasikan; v2 worker per situs; batasi upload executable |
| R-07 | ACME gagal karena port 80/DNS | Onboarding gagal | Sedang | Preflight check, pesan actionable, mode DNS-01 v1.1 |
| R-08 | Firewall mengunci admin | Downtime | Sedang | Commit-confirm, auto-revert, backup aturan |
| R-09 | Restore backup menimpa data baru | Kehilangan data | Rendah | Dry-run, backup pre-restore, swap direktori, checksum |
| R-10 | Dependency frontend membengkak | Performa/risiko | Sedang | Budget bundle, audit dependency, renovate terkontrol |
| R-11 | Perbedaan distro (SELinux, firewalld, nftables) | Bug instalasi | Tinggi | Capability detection, uji 4 distro, pesan jelas |
| R-12 | Terminal disalahgunakan | Kritis | Rendah | Nonaktif default root shell, re-auth, recording, role gate |

---

## 18. Pertanyaan Terbuka

| # | Pertanyaan | Default sementara |
|---|---|---|
| Q1 | Lisensi panel (AGPL-3.0, Apache-2.0, proprietary)? | Jangan tambahkan header lisensi sampai diputuskan |
| Q2 | Apakah panel harus dapat diekspos publik tanpa SSH tunnel? | Tidak; default localhost, opsi publik dengan TLS+2FA+allowlist |
| Q3 | Apakah email/DNS masuk roadmap 2027? | Tidak; fokus web server + operasional |
| Q4 | Apakah multi-node dibutuhkan untuk agensi? | v2; API menyertakan `node_id` sejak awal |
| Q5 | Apakah UI harus mendukung bahasa selain ID/EN? | Katalog i18n siap; terjemahan menyusul |
| Q6 | Apakah panel harus kompatibel dengan engine versi lama? | Hanya versi yang memenuhi kontrak §8.1 |

---

## 19. Definition of Done (v1.0)

- [ ] Instalasi bersih di Debian 12, Ubuntu 24.04, AlmaLinux 9 menghasilkan panel yang dapat diakses via tunnel.
- [ ] Setup pertama, login, 2FA, dan RBAC terbukti dengan e2e.
- [ ] Membuat situs + domain + HTTPS + publish `.ginxaccess` selesai dari UI tanpa SSH.
- [ ] Publish config invalid ditolak dengan baris/kolom; versi aktif tidak berubah.
- [ ] Rollback mengembalikan konten byte-identik dan tercatat di audit.
- [ ] File manager tidak dapat keluar dari docroot (test traversal + symlink).
- [ ] Firewall apply tidak pernah mengunci sesi (test commit-confirm).
- [ ] Backup + restore situs terverifikasi checksum dan dry-run.
- [ ] Semua endpoint mutasi tercatat di audit dengan aktor dan IP.
- [ ] `make check` hijau; e2e hijau; tidak ada critical/high pada audit.
- [ ] Dokumentasi admin, user, API, runbook tersedia.
- [ ] Paket `.deb` dan `.rpm` terpasang dan ter-upgrade di 4 distro target.

---

## 20. Lampiran A — Prompt Eksekusi (copy-paste ke agen coding)

~~~text
Anda adalah Principal Full-Stack Engineer yang membangun AGinX Control Panel.

KONTEKS
- Repositori: /home/kangpcode/wwwsite/aginx (modul Go "aginx", Go 1.24+).
- Engine: AGinX, web server native Go dengan konfigurasi per direktori ".ginxaccess".
  Baca docs/02-arsitektur.md dan docs/03-ginxaccess.md sebelum menulis kode.
- Panel berada di subdirektori control-panel/ dan memakai modul Go yang sama agar
  dapat meng-import aginx/internal/ginxaccess untuk validasi config.
- PRD lengkap ada di control-panel/PRD.md. Dokumen itu adalah sumber kebenaran.

MISI
Bangun AGinX Control Panel v1.0 sesuai PRD, dimulai dari M0 sampai M6 pada §14,
dengan UI yang terasa seperti aaPanel/OpenPanel tetapi native ke AGinX.

NON-NEGOTIABLE
1. Dua proses: aginx-panel (non-root, serve UI/API) dan aginx-paneld (root, agen RPC
   di unix socket). Tidak ada shell string; semua perintah memakai exec.Command
   dengan argumen array dan allowlist.
2. Validasi/publish .ginxaccess memakai parser engine, bukan parser duplikat.
   Publish atomik: temp -> fsync -> rename -> fsync dir -> reload -> probe ->
   auto-rollback jika gagal.
3. File manager memakai openat2 dengan RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS.
4. Password Argon2id, TOTP RFC 6238, sesi HttpOnly/Secure/SameSite=Strict, CSRF.
5. Semua mutasi tercatat di audit log ber-hash chain.
6. UI: React 19 + TypeScript + Vite + Tailwind, di-embed dengan go:embed.
   Token desain §6.4, shell §6.2, navigasi §6.1. Aksesibel WCAG 2.1 AA.
7. Bahasa UI Indonesia (id-ID) dan Inggris (en-US); tidak ada string hardcoded.
8. Tanpa Docker. Tanpa dependency runtime eksternal selain engine, systemd, dan
   tool sistem yang memang dikelola (mysql, nftables/ufw/firewalld).

URUTAN KERJA
M0 scaffolding + CI -> M1 auth/shell -> M2 situs + Config Studio -> M3 SSL/files/logs
-> M4 security/monitor/backup -> M5 database/cron/terminal/users -> M6 hardening/
packaging. Jangan melompati milestone; setiap milestone harus lulus exit criteria.

DELIVERABLE PER MILESTONE
- Kode production (bukan pseudo-code), error handling, komentar Bahasa Indonesia
  pada logika non-trivial.
- Migrasi SQL, test unit, test e2e untuk alur utama, dan dokumentasi singkat.
- Tidak ada TODO/FIXME di jalur produksi; tidak ada secret di repo.

GAYA KODE
- gofmt, go vet, error wrapping dengan %w, context propagation, tidak ada panic
  pada input pengguna, log terstruktur (slog) dengan request_id.
- Frontend: TypeScript strict, komponen kecil, TanStack Query, tanpa any.
- Commit kecil dan fokus; jangan refactor kode engine kecuali diperlukan kontrak.

ACCEPTANCE
Gunakan §19 Definition of Done. Jika engine belum menyediakan multi-site/TLS,
buat adapter + fake engine dan tandai fitur sebagai "degraded" di UI; jangan
mengarang kemampuan engine.

MULAI DENGAN
1. Tulis ADR stack di control-panel/docs/adr/0001-stack.md.
2. Scaffold Makefile, go.mod (modul sama), dua mode proses, health check.
3. Implementasikan M1 (auth + shell) dan tunjukkan cara menjalankannya.
~~~

## 21. Lampiran B — Checklist Acceptance MVP

- [ ] Setup token + owner pertama + TOTP.
- [ ] Login, logout, lockout, sesi tampil dan dapat dicabut.
- [ ] Sidebar/topbar/command palette sesuai §6.2.
- [ ] Dashboard menampilkan sistem + engine + situs (data nyata atau degraded).
- [ ] CRUD situs dengan wizard + preview config.
- [ ] Editor `.ginxaccess` + validasi baris/kolom + publish + rollback.
- [ ] Simulator request menampilkan hasil dan aturan yang match.
- [ ] SSL upload manual + force HTTPS.
- [ ] File manager browse/upload/edit/hapus dengan proteksi traversal.
- [ ] Log tail SSE per situs.
- [ ] Audit event untuk semua mutasi.
- [ ] `make check` dan e2e hijau.

## 22. Lampiran C — Referensi Internal

- `docs/01-bahasa.md` — keputusan bahasa dan model I/O engine.
- `docs/02-arsitektur.md` — arsitektur, fase request, struktur target engine.
- `docs/03-ginxaccess.md` — EBNF, directive, inheritance, batas keamanan, contoh.
- `docs/04-prototype.md` — implementasi POC dan operasional.
- `docs/05-roadmap.md` — roadmap engine; panel bergantung pada Fase 5–6.
- `docs/06-pengujian.md` — benchmark dan tuning sistem.
- `cmd/aginx/main.go` — flag, sinyal, lifecycle engine.
- `internal/ginxaccess/` — parser, cache, dan snapshot policy.
- `examples/spec/*.ginxaccess` — contoh bahasa penuh untuk template Config Studio.
