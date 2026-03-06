#!/bin/bash
echo "=== US Markets Analysis ==="
echo ""
echo "Checking learning database for US cities..."
echo ""

# Run the analyze-timing tool to see optimal entry windows
if [ -f "./analyze-timing" ]; then
    ./analyze-timing
elif [ -f "./cmd/analyze-timing/main.go" ]; then
    go run cmd/analyze-timing/main.go
else
    echo "Displaying database stats..."
    sqlite3 ./data/learning.db "SELECT city, COUNT(*) as records, AVG(high_temp) as avg_high FROM market_history GROUP BY city;"
fi
