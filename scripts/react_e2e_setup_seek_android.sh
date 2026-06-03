#!/usr/bin/env bash
set -exo pipefail

cd _seek

cat > ./android/app/src/main/res/values/config.xml <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="gmaps_api_key">xxx</string>
</resources>
EOF

mv config.example.js config.js
npm ci
npm run add-example-model
