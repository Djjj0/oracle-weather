# 🚨 Live Trading Bot - SAFETY GUIDE

## ⚠️ CRITICAL: Read Before Running

The bot is now configured for **LIVE TRADING** - it will place real orders with real money.

---

## 🎛️ Trading Mode Control

### Current Mode: **LIVE TRADING**

To switch between modes, edit `cmd/trade-bot/main.go` line 25:

```go
// Live trading mode (set to false for dry-run)
liveTrading = true   // Change to false for dry-run
```

Then rebuild:
```bash
go build -o bin/trade-bot.exe cmd/trade-bot/main.go
```

---

## ✅ Pre-Flight Checklist

Before running live trading, verify:

### 1. Configuration (.env file)
```bash
# Check your .env has these set:
POLYMARKET_PRIVATE_KEY=0x...    # Your wallet private key
POLYGON_RPC_URL=https://...     # Polygon RPC endpoint
VISUAL_CROSSING_API_KEY=...     # Weather API key
```

### 2. Wallet Balance
- Ensure you have sufficient USDC on Polygon for trades
- Minimum recommended: $500 USDC (for 5x $100 positions)
- Have some MATIC for gas fees (~$5-10 worth)

### 3. Test Connection
```bash
# Run this first to verify your Polymarket client works
cd /c/Users/djbro/.local/bin/oracle-weather
go run cmd/test-polymarket/main.go  # If this exists
```

### 4. Review Trade Logs
- Logs will be written to: `./data/logs/trades_YYYY-MM-DD.log`
- Monitor this file during operation

---

## 🚀 How to Start Live Trading

### Option 1: Foreground (Recommended for First Run)
```bash
cd /c/Users/djbro/.local/bin/oracle-weather
./bin/trade-bot.exe
```

**Watch for:**
- ✅ "Polymarket client initialized"
- ✅ "Resolution checker initialized"
- ⚠️ "LIVE TRADING MODE ENABLED"

### Option 2: Background (After Testing)
```bash
cd /c/Users/djbro/.local/bin/oracle-weather
nohup ./bin/trade-bot.exe > bot.out 2>&1 &
echo $! > bot.pid
```

To stop:
```bash
kill $(cat bot.pid)
```

---

## 📊 What the Bot Will Do

### Every 5 Minutes:
1. **Fetch active markets** from Polymarket
2. **Filter for weather markets** (temperature, rain, etc.)
3. **Check optimal entry time** (7-8 PM for US, 6 PM international)
4. **Fetch weather data**:
   - US cities: IEM (authoritative)
   - International: Weather Underground (scraper)
5. **Calculate expected profit** (after 3.15% fees)
6. **If profitable (>5%)**: Place market order
7. **Log everything** to `./data/logs/`

### When Opportunity Found:
```
🎯 OPPORTUNITY FOUND!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 Market: Will Chicago temperature exceed 32°F on February 23, 2026?
🌡️  Actual Temp: 45.2°F (IEM)
✅ Resolution: YES
💵 Current Price: $0.720
📈 Expected Return: 24.7%
💰 Expected Profit: $24.70
🔑 Token ID: 0x1234...

💸 EXECUTING LIVE TRADE...
   ✅ BUY YES @ $0.720
   Position Size: $100.00
   Expected P&L: +$24.70
   ✅ Trade executed successfully!
```

---

## 🛡️ Safety Features

### Built-in Protections:
1. ✅ **Min 5% profit threshold** (covers fees + margin)
2. ✅ **Max $100 per position** (limits exposure)
3. ✅ **Optimal entry timing** (learned from 1-year backfill)
4. ✅ **Data source validation** (VC vs WU comparison)
5. ✅ **Error logging** (captures failed trades)
6. ✅ **Graceful shutdown** (Ctrl+C to stop safely)

### What Could Go Wrong:
- ⚠️ **Network errors** - trade might fail mid-execution
- ⚠️ **Price slippage** - market moves before order fills
- ⚠️ **Gas spikes** - high gas costs reduce profit
- ⚠️ **Data delays** - WU scraper takes 12 seconds
- ⚠️ **Market closure** - market resolves before trade fills

---

## 📈 Expected Performance

### Conservative Estimates:
- **Markets scanned**: 8-12 weather markets daily
- **Opportunities**: 1-3 per day (10-30% hit rate)
- **Average profit**: $15-30 per trade (15-30% return)
- **Daily profit potential**: $30-90

### Best Case (with international markets):
- **Markets scanned**: 15-20 daily
- **Opportunities**: 3-5 per day
- **Daily profit potential**: $75-150

---

## 🔍 Monitoring

### Real-Time Logs:
```bash
# Watch trade logs
tail -f ./data/logs/trades_$(date +%Y-%m-%d).log
```

### Session Summary:
When you press Ctrl+C:
```
📊 SESSION SUMMARY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⏱️  Runtime: 2h 15m 30s
🔍 Total Scans: 27
🎯 Opportunities Found: 5
💸 Trades Executed: 5
💰 Total Profit: $127.50
📈 Avg Profit/Trade: $25.50
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## 🚨 Emergency Stop

### Immediate Shutdown:
Press `Ctrl+C` in the terminal

### Kill Background Process:
```bash
kill $(cat bot.pid)
```

### Cancel All Open Orders:
Log into Polymarket web app and manually cancel any open orders

---

## 🧪 Testing Recommendations

### Before Going Live:
1. **Run in dry-run mode for 24 hours**
   - Set `liveTrading = false`
   - Validate opportunities are real
   - Check timing is correct

2. **Test with small positions first**
   - Change `maxPositionSize = 10.0` (line 18)
   - Run for 48 hours
   - Verify actual vs expected profit

3. **Monitor closely**
   - Watch first 5-10 trades manually
   - Verify market resolutions match predictions
   - Check profit calculations accurate

4. **Scale gradually**
   - Start: $10 per trade (1 week)
   - Then: $25 per trade (1 week)
   - Then: $50 per trade (1 week)
   - Finally: $100+ per trade

---

## 📝 Trade Log Format

```
[2026-02-26 19:05:23] Chicago | YES | 45.2°F (IEM) | Token:0x1234... | $0.720 -> $24.70 profit | EXECUTED
[2026-02-26 19:10:15] London | NO | 48.5°F (WU) | Token:0x5678... | $0.650 -> $31.85 profit | EXECUTED
[2026-02-26 19:15:42] Seattle | YES | 52.1°F (IEM) | Token:0x9abc... | $0.800 -> $16.85 profit | FAILED: insufficient balance
```

---

## ⚙️ Configuration Options

### In `cmd/trade-bot/main.go`:

```go
// Adjust these values:
minProfitThreshold = 0.05   // 5% = conservative, 3% = aggressive
maxPositionSize    = 100.0  // Max $ per trade
checkInterval      = 5 * time.Minute  // How often to scan
liveTrading        = true   // false = dry run, true = live
```

### Rebuild After Changes:
```bash
go build -o bin/trade-bot.exe cmd/trade-bot/main.go
```

---

## 🎯 Success Criteria

### After 1 Week:
- ✅ 70%+ of trades should be profitable
- ✅ Average profit should be 15-25% per trade
- ✅ No critical errors or failed trades
- ✅ Market resolutions match predictions

### Red Flags:
- ❌ Multiple failed trades in a row
- ❌ Actual profit significantly lower than expected
- ❌ Market resolutions don't match weather data
- ❌ High gas costs eating into profit

---

## 💡 Tips for Success

1. **Run during peak hours**
   - US markets: 7-10 PM EST (most opportunities)
   - International: 6-9 PM local time

2. **Monitor first trades closely**
   - Verify data sources are correct
   - Check timing is optimal
   - Validate profit calculations

3. **Keep wallet funded**
   - Maintain $500+ USDC balance
   - Keep $10-20 MATIC for gas

4. **Review logs daily**
   - Check for patterns
   - Identify improvements
   - Adjust thresholds if needed

---

## 📞 Support

### If Something Goes Wrong:
1. **Stop the bot** (Ctrl+C)
2. **Check logs**: `./data/logs/trades_*.log`
3. **Review errors**: Look for ERROR or FAILED entries
4. **Test in dry-run**: Set `liveTrading = false`
5. **Report issues**: Check error messages, network connectivity

---

## ✅ Ready to Start?

```bash
cd /c/Users/djbro/.local/bin/oracle-weather
./bin/trade-bot.exe
```

**Good luck! 🚀**

---

**Disclaimer**: Trading involves risk. Past performance does not guarantee future results. Only trade with money you can afford to lose. Monitor the bot actively, especially during initial operation.
