# patchbay

[English Version](README.md)

Port forwarder TCP/UDP modern dan alternatif HAProxy yang simpel dengan Web UI.
Tersedia sebagai aplikasi/service Windows mandiri atau sebagai **kontainer Docker** untuk server Linux.
Dibuat menggunakan Go murni dengan web dashboard Vanilla JS tersemat (*embedded*), *realtime streaming* Server-Sent Events (SSE), pencatatan trafik langsung (*live traffic logging*), dan kemampuan pembaruan otomatis melalui GitHub Releases.

## Fitur Utama

- **Port Forwarding TCP dan UDP** (bisa keduanya dalam satu aturan)
- **Web Dashboard Realtime** menggunakan **Server-Sent Events (SSE)** dan **Vanilla JS** murni
- **Grafik Bandwidth & Throughput Langsung** berbasis **HTML5 Canvas** (tanpa pustaka/library eksternal)
- **Pencatat Sesi Koneksi (Traffic Logger)** dengan *in-memory ring buffer* dan rotasi file harian otomatis (`.jsonl`)
- **Ikon System Tray Bawaan (Windows)**: Buka Dashboard, Cek Pembaruan, Kontrol Windows Service, Keluar
- **Mode Windows Service (Opsional)**: Forwarding dan dashboard tetap berjalan saat komputer *reboot* tanpa perlu login pengguna
- **Pembaruan Otomatis (Auto-Update)**: Mendeteksi rilis baru di GitHub dengan tombol instalasi dan restart otomatis
- **Konfigurasi Tersimpan Permanen**: Lokasi konfigurasi bersama di `%ProgramData%\patchbay` pada Windows atau volume mount pada Docker
- **Autentikasi HTTP Basic Auth (Opsional)**: amankan akses dashboard dan API dengan environment variable (`PATCHBAY_AUTH_USER` & `PATCHBAY_AUTH_PASS`)
- **Nol Dependensi Eksternal**: Seluruh fungsi dibangun dengan Go stdlib dan Win32 API native

---

## Menjalankan dengan Docker (Linux / Server)

Patchbay dapat berjalan di Docker sebagai alternatif HAProxy yang ringan, visual, dan mudah diatur.

### Opsi 1: Host Networking (Direkomendasikan)
Menggunakan `network_mode: host` memungkinkan Patchbay mengelola dan membuka port apa saja di mesin host secara dinamis tanpa perlu mendaftarkan `-p <port>:<port>` terlebih dahulu:

```yaml
# docker-compose.yml
services:
  patchbay:
    image: ghcr.io/fadlee/patchbay:latest
    container_name: patchbay
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./data:/app
    environment:
      - PATCHBAY_HOST=0.0.0.0 # Bind dashboard ke semua antarmuka jaringan
      # Autentikasi Opsional:
      # - PATCHBAY_AUTH_USER=admin
      # - PATCHBAY_AUTH_PASS=passwordanda
```

Atau menggunakan perintah `docker run`:
```bash
docker run -d \
  --name patchbay \
  --restart unless-stopped \
  --network host \
  -v $(pwd)/data:/app \
  -e PATCHBAY_HOST=0.0.0.0 \
  ghcr.io/fadlee/patchbay:latest
```

> **Catatan:** Dengan mode `host`, setiap aturan forward yang Anda tambahkan di Web UI (`http://<server-ip>:8787`) akan langsung aktif dan mendengarkan port tersebut di mesin host Linux Anda.

---

### Opsi 2: Bridge Networking (Port Mapping Standar)
Jika lingkungan Docker Anda tidak mendukung atau tidak ingin menggunakan host network:

```yaml
services:
  patchbay:
    image: ghcr.io/fadlee/patchbay:latest
    container_name: patchbay
    restart: unless-stopped
    ports:
      - "8787:8787" # Admin Dashboard
      # Daftarkan port forwarding yang ingin dibuka ke host:
      - "8080:8080"
      - "5433:5433"
    volumes:
      - ./data:/app
    environment:
      - PATCHBAY_HOST=0.0.0.0
      # Autentikasi Opsional:
      # - PATCHBAY_AUTH_USER=admin
      # - PATCHBAY_AUTH_PASS=passwordanda
```

---

### Routing ke Target Host / Kontainer Lain
- Untuk mem-forward trafik ke service yang berjalan langsung di host Linux dari kontainer bridge: gunakan target `host.docker.internal` (dengan menambahkan `extra_hosts: ["host.docker.internal:host-gateway"]`).
- Jika menggunakan `network_mode: host`, target ke service lokal di mesin host cukup menggunakan `127.0.0.1`.


### Autentikasi Dashboard (Basic Auth)
Lindungi dashboard dan REST API dengan menyetel environment variable:
```bash
-e PATCHBAY_AUTH_USER=admin -e PATCHBAY_AUTH_PASS=passwordanda
```
Jika variabel tersebut dikosongkan atau tidak disetel, autentikasi otomatis nonaktif (perilaku bawaan desktop).

---

## Kompilasi & Build (Windows / Lokal)

Membutuhkan Go 1.22+ dan [Task](https://taskfile.dev).

```bash
# Build binary Windows mandiri (termasuk manifest & icon tray):
task build-windows

# Build installer setup Windows NSIS (dist/patchbay-setup-amd64.exe):
task build-installer

# Build binary dev lokal untuk Linux / macOS:
task build
```

---

## Menjalankan di Windows

Cukup klik dua kali `patchbay.exe`. Aplikasi akan berjalan senyap di System Tray — klik kanan ikon untuk **Open Dashboard** atau **Quit**. Dashboard default berada di `http://127.0.0.1:8787`.

Penyimpanan konfigurasi dan log di Windows:
- Konfigurasi: `%ProgramData%\patchbay\portforward-config.json`
- Log Trafik: `%ProgramData%\patchbay\logs\traffic-YYYY-MM-DD.jsonl`

---

## Mode Windows Service

Mode service memungkinkan port forwarding dan web dashboard berjalan otomatis saat komputer menyala (*boot*), bahkan sebelum ada pengguna yang login.

- **Aktifkan:** Klik kanan ikon tray → **Enable service mode**. Windows akan meminta konfirmasi UAC (Administrator), lalu service akan dipasang dan dijalankan otomatis.
- **Setelah Reboot:** Service langsung aktif kembali dan menjalankan semua aturan yang berstatus aktif (`Enabled == true`).
- **Keluar:** Di mode service, menu **Quit** pada tray hanya menutup tampilan tray; engine forwarding dan dashboard tetap berjalan di latar belakang.
- **Nonaktifkan:** Klik kanan ikon tray → **Disable service mode**. Service akan dihentikan dan dihapus, lalu aplikasi kembali ke mode tray lokal biasa.
