#!/usr/bin/env python
"""Test the WU scraper and print results"""

import sys
from scrape_wunderground import scrape_wunderground

if __name__ == '__main__':
    station = 'EGLL'  # London Heathrow
    date = '2026-02-24'
    freq = 'daily'

    print(f"=== Testing WU Scraper ===")
    print(f"Station: {station}")
    print(f"Date: {date}")
    print(f"Frequency: {freq}")
    print()

    print("Fetching data from Weather Underground...")
    print("(This takes ~5-10 seconds)")
    print()

    try:
        df = scrape_wunderground(station, date, freq)

        if df is not None and not df.empty:
            print("✅ Success! Data retrieved:")
            print()
            print(df)
            print()

            if 'Temperature_High' in df.columns:
                high_temp = df['Temperature_High'].iloc[0]
                print(f"📈 High Temperature: {high_temp}°F ({(high_temp-32)*5/9:.1f}°C)")

            if 'Temperature_Low' in df.columns:
                low_temp = df['Temperature_Low'].iloc[0]
                print(f"📉 Low Temperature: {low_temp}°F ({(low_temp-32)*5/9:.1f}°C)")
        else:
            print("❌ No data returned")

    except Exception as e:
        print(f"❌ Error: {e}")
        import traceback
        traceback.print_exc()
