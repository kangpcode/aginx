#!/usr/bin/env python3
"""Uji binary AGinX melalui TCP dan sinyal, dengan docroot sementara."""

import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin/aginx"


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def publish(root, text):
    temporary = root / ".ginxaccess.next"
    temporary.write_text(text)
    temporary.replace(root / ".ginxaccess")


def policy(generation):
    return (
        f'header set X-Generation "{generation}";\n'
        'rewrite "^/welcome$" "/index.html" last;\n'
        'rewrite "^/leak$" "/private/secret.html" last;\n'
    )


def startup(process, log):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        log.seek(0)
        match = re.search(r"AGinX siap.*listen=127[.]0[.]0[.]1:(\d+)", log.read())
        if match:
            return int(match[1])
        require(process.poll() is None, "daemon berhenti sebelum siap")
        time.sleep(0.01)
    raise AssertionError("daemon tidak siap dalam 5 detik")


def main():
    require(BINARY.is_file(), "jalankan make build terlebih dahulu")
    checks = []
    with tempfile.TemporaryDirectory(prefix="aginx-live-smoke-") as directory:
        work = Path(directory)
        root = work / "www"
        root.mkdir()
        (root / "index.html").write_bytes(b"hello AGinX\n")
        (root / "private").mkdir()
        (root / "private/secret.html").write_text("SECRET")
        (root / "private/.ginxaccess").write_text("deny all;\n")
        large = b"AGinX-live-sendfile\n" * 524288
        (root / "large.bin").write_bytes(large)
        publish(root, policy("one"))

        valid = subprocess.run(
            [str(BINARY), "-t", "-config", str(root / ".ginxaccess")],
            capture_output=True, text=True, timeout=5,
        )
        require(valid.returncode == 0, f"CLI -t gagal: {valid.stderr}")
        invalid = work / "invalid.conf"
        invalid.write_text("unknown_directive;\n")
        for filename in (invalid, work / "missing.conf"):
            result = subprocess.run(
                [str(BINARY), "-t", "-config", str(filename)],
                capture_output=True, text=True, timeout=5,
            )
            require(result.returncode != 0, "CLI menerima config invalid/missing")
            if filename == invalid:
                require("baris 1" in result.stderr, "error parser tanpa nomor baris")
        checks.append("CLI valid/invalid/missing")

        with (work / "daemon.log").open("w+") as log:
            process = subprocess.Popen(
                [str(BINARY), "-listen", "127.0.0.1:0", "-root", str(root),
                 "-config-ttl", "20ms"],
                stdout=subprocess.DEVNULL, stderr=log,
            )
            client = None
            try:
                port = startup(process, log)
                client = http.client.HTTPConnection("127.0.0.1", port, timeout=5)

                def request(target, method="GET"):
                    client.request(method, target)
                    response = client.getresponse()
                    return response.status, response.headers, response.read()

                status, headers, body = request("/welcome")
                require(status == 200 and body == b"hello AGinX\n", "rewrite gagal")
                require(headers.get("X-Generation") == "one", "header config hilang")
                connection = client.sock
                status, headers, body = request("/index.html", "HEAD")
                require(status == 200 and body == b"", "HEAD mengirim body")
                require(int(headers["Content-Length"]) == 12, "panjang HEAD salah")
                require(client.sock is connection, "keep-alive membuka socket baru")
                checks.append("TCP GET/HEAD/keep-alive/rewrite/header")

                for target in ("/private", "/private/secret.html", "/leak",
                               "/%2e%2e/etc/passwd", "/.ginxaccess"):
                    require(request(target)[0] == 403, f"akses lolos: {target}")
                checks.append("ACL sumber/tujuan dan traversal")

                publish(root, policy("two"))
                deadline = time.monotonic() + 2
                while True:
                    status, headers, _ = request("/welcome")
                    require(status == 200, "update atomic memutus layanan")
                    if headers.get("X-Generation") == "two":
                        break
                    require(time.monotonic() < deadline, "TTL gagal mendeteksi update")
                    time.sleep(0.01)
                require(process.poll() is None, "daemon restart saat update")
                checks.append("update atomic tanpa reload/restart")

                publish(root, "unknown_directive;\n")
                time.sleep(0.05)
                status, headers, _ = request("/welcome")
                require(status == 200 and headers.get("X-Generation") == "two",
                        "config rusak kehilangan last-known-good")
                process.send_signal(signal.SIGHUP)
                time.sleep(0.05)
                require(request("/welcome")[0] == 200, "SIGHUP memutus layanan")
                publish(root, policy("three"))
                deadline = time.monotonic() + 2
                while request("/welcome")[1].get("X-Generation") != "three":
                    require(time.monotonic() < deadline, "config tidak pulih")
                    time.sleep(0.01)
                log.seek(0)
                require("ginxaccess tidak valid" in log.read(), "error config tidak dicatat")
                checks.append("last-known-good/SIGHUP/recovery")

                with socket.create_connection(("127.0.0.1", port), timeout=5) as raw:
                    raw.sendall(b"GET / HTTP/1.1\r\nHost: test\r\n"
                                b"Content-Length: 0\r\nTransfer-Encoding: chunked\r\n\r\n")
                    response = http.client.HTTPResponse(raw)
                    response.begin()
                    require(response.status == 400, "framing ambigu diterima")
                    require(response.getheader("Connection") == "close", "error tanpa close")
                    response.read()
                    response.close()
                    require(raw.recv(1) == b"", "socket malformed tidak ditutup")
                checks.append("framing CL+TE ditolak dan socket ditutup")

                # Baca header dahulu, lalu SIGTERM ketika body besar masih dikirim.
                client.request("GET", "/large.bin")
                response = client.getresponse()
                require(response.status == 200, "transfer besar gagal")
                process.send_signal(signal.SIGTERM)
                actual = response.read()
                require(hashlib.sha256(actual).digest() == hashlib.sha256(large).digest(),
                        "SIGTERM memotong body response aktif")
                require(process.wait(timeout=12) == 0, "daemon tidak berhenti bersih")
                checks.append("SIGTERM drain dan checksum file besar")
            except Exception:
                log.seek(0)
                print(log.read(), end="", file=sys.stderr)
                raise
            finally:
                if client is not None:
                    client.close()
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=12)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)

    print(json.dumps({"result": "PASS", "checks": checks,
                      "large_file_bytes": len(large)}, indent=2))


if __name__ == "__main__":
    main()
