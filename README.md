# Oracle Weather

**Weather prediction trading bot for Polymarket using oracle lag arbitrage**

## Strategy

This bot identifies weather markets on Polymarket where:
1. The resolution time has passed
2. Official weather data (IEM ASOS) is available
3. The market hasn't updated yet (oracle lag)
4. There's sufficient edge (15%+ by default)

## How It Works

1. **Data Source**: Uses IEM (Iowa Environmental Mesonet) ASOS data - the exact same source as Weather Underground, which Polymarket uses for resolution
2. **Market Detection**: Scans for weather markets (temperature, rain, etc.) past their resolution time
3. **Edge Calculation**: Calculates edge on both YES and NO sides, picks the best
4. **Smart Filters**:
   - Dead market filter (skip prices < $0.05)
   - Adjacent bucket exclusion (don't bet NO on temperature ranges near actual temp)
   - Liquidity/volume thresholds
5. **Position Sizing**: Scales with both confidence AND edge (Kelly-like approach)

## Data Accuracy

- **IEM ASOS**: 98% confidence - exact match to Polymarket's resolution source
- **Airport mapping**: Major cities mapped to nearest airport weather stations
- **Temperature rounding**: Matches Polymarket's nearest-integer rounding

## Configuration

Edit `.env`:

```env
# Strategy Settings
MIN_PROFIT_THRESHOLD=0.15  # 15% minimum edge
MAX_POSITION_SIZE=5        # $5 max per trade

# Market Filters
MIN_LIQUIDITY=1000         # $1k minimum
MIN_VOLUME=10000           # $10k minimum
```

## Running

```bash
# Build
go build -o bin/oracle-weather cmd/bot/main.go

# Run
./bin/oracle-weather
```

## Expected Performance

- **Win Rate**: 90-95% (data accuracy)
- **Trades/Day**: 2-5 (with 15% threshold)
- **Daily Profit**: $10-20 (with $5 positions)

## Risk Management

- 15% edge minimum (covers fees + slippage + variance)
- Dead market filter prevents false opportunities
- Adjacent bucket exclusion prevents hedging losses
- Circuit breaker: $500 loss or 50 trades/day

## Dependencies

- Go 1.22+
- Python 3.8+ (for Polymarket SDK)
- Alchemy API key (for Polygon RPC)
- Polymarket API credentials
