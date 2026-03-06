# Get Visual Crossing API Key (2 minutes)

## Step 1: Sign Up
Go to: https://www.visualcrossing.com/weather-api

Click **"Sign Up"** in top right

## Step 2: Create Free Account
- Enter email address
- Choose password
- Select **"FREE"** plan (1,000 records/day)
- No credit card required ✅

## Step 3: Verify Email
Check your email for verification link and click it

## Step 4: Get API Key
1. Log in to: https://www.visualcrossing.com/account
2. Your API key is shown in the dashboard
3. Copy it

## Step 5: Set Environment Variable

### Windows (PowerShell):
```powershell
$env:VISUAL_CROSSING_API_KEY="your_key_here"
```

### Windows (Git Bash/MSYS):
```bash
export VISUAL_CROSSING_API_KEY="your_key_here"
```

### Permanent (add to .env file):
```bash
echo 'VISUAL_CROSSING_API_KEY=your_key_here' >> ~/.local/bin/oracle-weather/.env
```

## Step 6: Test It
```bash
cd /c/Users/djbro/.local/bin/oracle-weather
go run cmd/test-visualcrossing/main.go
```

## Troubleshooting

**"API key not set"**:
```bash
# Check if set:
echo $VISUAL_CROSSING_API_KEY

# If empty, export it again
export VISUAL_CROSSING_API_KEY="your_key_here"
```

**"Invalid API key"**:
- Make sure you copied the entire key
- No spaces before/after
- Check if you verified your email

**"Rate limit exceeded"**:
- Free tier = 1,000 requests/day
- Wait 24 hours or upgrade plan

## Free Tier Limits

- ✅ 1,000 records/day
- ✅ Hourly historical data
- ✅ 50+ years of history
- ✅ Worldwide coverage
- ✅ No credit card required

For our backfill:
- 8 cities × 365 days = 2,920 records
- Takes 3 days with free tier
- Or upgrade temporarily for $0.0001/record = $0.29 total
