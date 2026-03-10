#!/bin/bash
cd "$(dirname "$0")"
./bin/bot.exe 2>&1 | grep -v "DevTools\|ERROR:.*\(direct_composition\|ssl_client_socket\|registration_request\|mcs_client\|gcm\)\|handshake failed\|GetGpuDriverOverlayInfo\|DEPRECATED_ENDPOINT\|PHONE_REGISTRATION_ERROR\|Authentication Failed\|TensorFlow\|Created TensorFlow"
