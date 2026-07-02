#!/bin/bash
# Create a stable self-signed code-signing identity ("wspr-dev") so the macOS
# permissions (Input Monitoring, Accessibility, Microphone) granted to wspr.app
# survive rebuilds. With ad-hoc signing every rebuild looks like a new app and
# TCC revokes the grants; a stable certificate keeps the identity constant.
#
# Idempotent: does nothing if the identity already exists.
set -e

IDENTITY="wspr-dev"

if security find-identity -p codesigning 2>/dev/null | grep -q "\"$IDENTITY\""; then
    echo "code-signing identity '$IDENTITY' already present"
    exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/cfg" <<'EOF'
[req]
distinguished_name = dn
x509_extensions = v3
prompt = no
[dn]
CN = wspr-dev
[v3]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
EOF

/usr/bin/openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$TMP/key.pem" -out "$TMP/cert.pem" \
    -days 3650 -config "$TMP/cfg" >/dev/null 2>&1

# A non-empty p12 password avoids a MAC-verification quirk in `security import`.
/usr/bin/openssl pkcs12 -export \
    -inkey "$TMP/key.pem" -in "$TMP/cert.pem" \
    -name "$IDENTITY" -out "$TMP/id.p12" -passout pass:wspr >/dev/null 2>&1

# -A allows apps to use the key; the first codesign still asks once.
security import "$TMP/id.p12" -P wspr -A >/dev/null 2>&1

echo "created code-signing identity '$IDENTITY'"
