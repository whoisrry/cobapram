#!/bin/sh
# Membuat .env dari .env.example: secret acak dan alamat IP server.
# Pemakaian: scripts/gen-env.sh [IP]   (dipanggil oleh `make env IP=...`)
set -eu
cd "$(dirname "$0")/.."

if [ -f .env ]; then
	echo ".env sudah ada dan tidak ditimpa." >&2
	echo "Password database tersimpan di volume Postgres; jika secret diganti, app tidak bisa login ke DB." >&2
	echo "Edit .env secara manual, atau hapus .env (dan volume dengan 'docker compose down -v') untuk mulai dari nol." >&2
	exit 1
fi

ip="${1:-}"
if [ -z "$ip" ]; then
	ip=$(hostname -I 2>/dev/null | awk '{print $1}') || true
fi
if [ -z "$ip" ]; then
	ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit }}') || true
fi
if [ -z "$ip" ]; then
	echo "IP server tidak terdeteksi. Jalankan: make env IP=<ip-server>" >&2
	exit 1
fi

# 32 byte acak dalam heksadesimal; aman dipakai di URL dan file .env.
secret() { od -An -N32 -tx1 /dev/urandom | tr -d ' \n'; }

umask 077
sed \
	-e "s|^SITE_ADDRESS=.*|SITE_ADDRESS=$ip|" \
	-e "s|^JWT_ISSUER=.*|JWT_ISSUER=https://$ip|" \
	-e "s|^DB_PASSWORD=.*|DB_PASSWORD=$(secret)|" \
	-e "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=$(secret)|" \
	-e "s|^JWT_SECRET=.*|JWT_SECRET=$(secret)|" \
	-e "s|^CSRF_KEY=.*|CSRF_KEY=$(secret)|" \
	.env.example > .env

echo ".env dibuat (mode 600) untuk alamat $ip"
echo "Jika IP salah: hapus .env lalu jalankan 'make env IP=<ip-server>'."
