# ChromeDriver Setup Guide

## Quick Install (Windows)

### Step 1: Check Chrome Version
1. Open Chrome browser
2. Click menu (3 dots) → Help → About Google Chrome
3. Note your version (e.g., "131.0.6778.140")

### Step 2: Download ChromeDriver
1. Go to: https://googlechromelabs.github.io/chrome-for-testing/
2. Find "Stable" channel
3. Download ChromeDriver for your Chrome version (Windows 64-bit)
4. Extract `chromedriver.exe`

### Step 3: Install ChromeDriver
```bash
# Option A: Move to project directory
mkdir -p /c/Users/djbro/.local/bin/oracle-weather/bin
mv chromedriver.exe /c/Users/djbro/.local/bin/oracle-weather/bin/

# Option B: Move to system PATH
mv chromedriver.exe /c/Windows/System32/

# Verify installation
chromedriver --version
```

## Alternative: Automated Install

```bash
# Download latest ChromeDriver (adjust URL for your Chrome version)
cd /c/Users/djbro/.local/bin/oracle-weather
mkdir -p bin
cd bin

# For Chrome 131 (adjust version as needed)
curl -O https://storage.googleapis.com/chrome-for-testing-public/131.0.6778.85/win64/chromedriver-win64.zip

# Extract
unzip chromedriver-win64.zip
mv chromedriver-win64/chromedriver.exe ./
rm -rf chromedriver-win64 chromedriver-win64.zip

# Add to PATH (for current session)
export PATH=$PATH:/c/Users/djbro/.local/bin/oracle-weather/bin

# Verify
chromedriver --version
```

## Troubleshooting

### "ChromeDriver version mismatch"
- ChromeDriver version MUST match your Chrome version
- If Chrome is 131.x, download ChromeDriver 131.x
- Update Chrome or ChromeDriver to match

### "ChromeDriver not found"
```bash
# Add to PATH permanently
echo 'export PATH=$PATH:/c/Users/djbro/.local/bin/oracle-weather/bin' >> ~/.bashrc
source ~/.bashrc
```

### "Chrome failed to start"
- Close all Chrome windows
- Try running as administrator
- Check Chrome installation path

## Next Steps

After ChromeDriver is installed:
```bash
# Test WU scraper
go run cmd/scrape-wu/main.go --city London --date 2026-02-24

# If it works, proceed with implementation
```
