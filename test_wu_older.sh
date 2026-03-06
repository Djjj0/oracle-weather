#!/bin/bash
# Fetch WU data for 30 days ago
DATE=$(date -d '30 days ago' +%Y/%m/%d 2>/dev/null || date -v-30d +%Y/%m/%d 2>/dev/null || echo "2026/1/26")
URL="https://www.wunderground.com/history/daily/EGLL/date/$DATE"
echo "Fetching: $URL"
curl -s -A "Mozilla/5.0" "$URL" | grep -o '"observations":\[{[^]]*}\]' | head -c 5000
