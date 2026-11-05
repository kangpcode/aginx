# AGinX

Web server native mandiri dalam Go, dengan konfigurasi per direktori `.ginxaccess`. Implementasi saat ini adalah POC Linux: HTTP/1.0 dan HTTP/1.1 untuk GET/HEAD, keep-alive/pipelining tanpa request body, static files, directory index, ACL IP/CIDR, custom header, rewrite, cache metadata dengan TTL, SIGHUP, dan graceful shutdown.

Tidak memerlukan instalasi Apache/Nginx. Tidak ada dependency Go di luar standard library.

```bash
make check
make smoke
./bin/aginx -t -config examples/www/.ginxaccess
./bin/aginx -listen 127.0.0.1:8080 -root ./examples/www
```

Di terminal lain:

```bash
curl -i http://127.0.0.1:8080/
curl -i http://127.0.0.1:8080/welcome
curl --path-as-is -i http://127.0.0.1:8080/%2e%2e/etc/passwd
```

Perubahan konfigurasi terdeteksi pada validasi metadata berikutnya, default 100 ms saat ada request. Tulis konfigurasi ke temporary file pada direktori yang sama kemudian gunakan atomic rename. File rusak mempertahankan snapshot valid terakhir yang masih berada dalam cache; file pertama yang rusak menghasilkan 503.

`make smoke` memerlukan Python 3 dan izin socket TCP loopback. Pengujian menjalankan binary pada port sementara dengan docroot sementara, menguji update config, last-known-good, SIGHUP, dan drain transfer 10 MiB saat SIGTERM. Tidak mengubah docroot contoh atau memasang service. Jalur syscall `sendfile`, termasuk partial transfer dan EAGAIN, sudah teramati pada verifikasi lokal; lihat [laporan verifikasi](docs/VERIFIKASI.md).

Dokumentasi sesuai urutan [CLAUDE.md](CLAUDE.md):

1. [Keputusan bahasa dan model I/O](docs/01-bahasa.md).
2. [Arsitektur dan struktur proyek](docs/02-arsitektur.md).
3. [Spesifikasi `.ginxaccess`, EBNF, dan lima contoh](docs/03-ginxaccess.md).
4. [POC, kode lengkap, build, dan pengoperasian](docs/04-prototype.md).
5. [Roadmap dan checklist dengan estimasi](docs/05-roadmap.md).
6. [Pengujian, benchmark, dan tuning](docs/06-pengujian.md).

[Hasil verifikasi lokal](docs/VERIFIKASI.md) membedakan hasil yang sudah dijalankan dengan target yang belum diukur. Target 100K koneksi, overhead <5%, dan throughput kompetitif terhadap Nginx belum dibuktikan. TLS, master-worker, inotify, HTTP/2, FastCGI, serta upgrade binary belum diimplementasikan. Contoh spesifikasi lengkap dalam `examples/spec/` membutuhkan engine lanjutan; parser POC menolak directive yang belum didukung.

Toolchain minimum deklaratif Go 1.24; gunakan patch terbaru dari cabang Go yang masih didukung. Build lokal menggunakan Go 1.27.0. Target Linux kernel >=4.18 perlu diuji pada VM distro target. Service unit contoh berada di [packaging/systemd](packaging/systemd/aginx.service); belum dipasang ke sistem. Lisensi distribusi belum dipilih pemilik proyek.
