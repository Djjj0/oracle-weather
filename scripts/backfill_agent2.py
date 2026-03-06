#!/usr/bin/env python3
"""
backfill_agent2.py
Backfills 365 days of historical IEM ASOS peak-hour data for 5 cities
into the learning databases.

Cities:
  US DB  (data/learning.db):           New York (KLGA), Atlanta (KATL)
  Intl DB (data/learning_international.db): Ankara (LTAC), Munich (EDDM), Sao Paulo (SBGR)
"""

import sqlite3
import time
import os
import requests
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo   # Python 3.9+
from io import StringIO

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
# Resolve project root relative to this script's location (scripts/ subdir)
PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
US_DB        = os.path.join(PROJECT_ROOT, "data", "learning.db")
INTL_DB      = os.path.join(PROJECT_ROOT, "data", "learning_international.db")

# Date range: 365 days ending yesterday (2026-03-05 if today is 2026-03-06)
END_DATE   = datetime(2026, 3, 5)
START_DATE = END_DATE - timedelta(days=364)   # 365 days inclusive

CITIES = [
    # (city_name, iem_station, tz_string, db_path)
    ("New York",  "KLGA", "America/New_York",    US_DB),
    ("Atlanta",   "KATL", "America/New_York",    US_DB),
    ("Ankara",    "LTAC", "Europe/Istanbul",      INTL_DB),
    ("Munich",    "EDDM", "Europe/Berlin",        INTL_DB),
    ("Sao Paulo", "SBGR", "America/Sao_Paulo",   INTL_DB),
]

IEM_URL = "https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py"

# ---------------------------------------------------------------------------
# Fetch IEM data for a station (full year in one request)
# ---------------------------------------------------------------------------
def fetch_iem(station: str, start: datetime, end: datetime) -> list[dict]:
    """
    Fetch hourly tmpf observations for `station` from IEM ASOS.
    Returns list of {'valid_utc': datetime, 'tmpf': float}.
    Skips rows where tmpf is 'null' or non-numeric.
    """
    params = {
        "station":     station,
        "data":        "tmpf",
        "year1":       start.year,
        "month1":      start.month,
        "day1":        start.day,
        "year2":       end.year,
        "month2":      end.month,
        "day2":        end.day,
        "tz":          "UTC",
        "format":      "onlycomma",
        "latlon":      "no",
        "elev":        "no",
        "missing":     "null",
        "trace":       "null",
        "direct":      "no",
        "report_type": ["3", "4"],   # requests sends multiple values
    }
    print(f"  Fetching IEM data for {station} ({start.date()} to {end.date()})...")
    resp = requests.get(IEM_URL, params=params, timeout=120)
    resp.raise_for_status()

    rows = []
    lines = resp.text.splitlines()
    # Skip header line(s) — header starts with 'station'
    data_started = False
    for line in lines:
        line = line.strip()
        if not line:
            continue
        if line.startswith("station"):
            data_started = True
            continue
        if not data_started:
            continue
        parts = line.split(",")
        if len(parts) < 3:
            continue
        _station, valid_str, tmpf_str = parts[0], parts[1], parts[2]
        if tmpf_str in ("null", "M", ""):
            continue
        try:
            tmpf = float(tmpf_str)
        except ValueError:
            continue
        # Parse UTC timestamp
        try:
            valid_utc = datetime.strptime(valid_str, "%Y-%m-%d %H:%M").replace(tzinfo=timezone.utc)
        except ValueError:
            try:
                valid_utc = datetime.strptime(valid_str, "%Y-%m-%dT%H:%M").replace(tzinfo=timezone.utc)
            except ValueError:
                continue
        rows.append({"valid_utc": valid_utc, "tmpf": tmpf})

    print(f"  Got {len(rows)} valid observations for {station}")
    return rows


# ---------------------------------------------------------------------------
# Group observations by local calendar date
# ---------------------------------------------------------------------------
def group_by_local_date(rows: list[dict], tz_str: str) -> dict:
    """
    Returns {local_date_str: [row, ...]} where local_date is computed by
    converting each UTC timestamp to the city's local timezone.
    """
    tz = ZoneInfo(tz_str)
    by_date = {}
    for row in rows:
        local_dt = row["valid_utc"].astimezone(tz)
        date_str = local_dt.strftime("%Y-%m-%d")
        by_date.setdefault(date_str, []).append(row)
    return by_date


# ---------------------------------------------------------------------------
# Helper: Fahrenheit → Celsius
# ---------------------------------------------------------------------------
def f_to_c(f: float) -> float:
    return round((f - 32) * 5 / 9, 2)


# ---------------------------------------------------------------------------
# Insert market_history rows and city_stats for one city
# ---------------------------------------------------------------------------
def process_city(city: str, station: str, tz_str: str, db_path: str,
                 start: datetime, end: datetime):
    print(f"\n=== Processing {city} ({station}) ===")

    # Fetch raw data
    rows = fetch_iem(station, start, end)
    if not rows:
        print(f"  WARNING: No data returned for {station}. Skipping.")
        return

    # Group by local date
    by_date = group_by_local_date(rows, tz_str)

    tz = ZoneInfo(tz_str)

    conn = sqlite3.connect(db_path)
    cur  = conn.cursor()

    high_temp_hours   = []  # local hour of daily max
    iem_final_hours   = []  # local hour of last obs of day
    inserted          = 0

    # Iterate over the date range (only dates we have data for)
    all_dates = sorted(by_date.keys())
    for date_str in all_dates:
        day_rows = by_date[date_str]
        if not day_rows:
            continue

        # Find the observation with max tmpf
        max_row = max(day_rows, key=lambda r: r["tmpf"])
        # Last observation of the day (latest valid_utc)
        last_row = max(day_rows, key=lambda r: r["valid_utc"])

        high_utc  = max_row["valid_utc"]
        last_utc  = last_row["valid_utc"]
        tmpf_max  = max_row["tmpf"]
        tmpc_max  = f_to_c(tmpf_max)

        # Local hour of peak
        high_local = high_utc.astimezone(tz)
        last_local = last_utc.astimezone(tz)
        high_hour  = high_local.hour + high_local.minute / 60.0
        last_hour  = last_local.hour + last_local.minute / 60.0

        high_temp_hours.append(high_hour)
        iem_final_hours.append(last_hour)

        # optimal_entry_time = high_temp_time + 1 hour (UTC)
        optimal_utc = high_utc + timedelta(hours=1)

        market_id = f"{city}_{date_str}"

        cur.execute("""
            INSERT OR REPLACE INTO market_history
                (market_id, city, date, timezone, high_temp, high_temp_time,
                 iem_data_final_time, market_resolved_time, optimal_entry_time,
                 data_lag_minutes, resolution_lag_minutes, success, notes)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            market_id,
            city,
            date_str,
            tz_str,
            tmpc_max,
            high_utc.strftime("%Y-%m-%d %H:%M:%S"),
            last_utc.strftime("%Y-%m-%d %H:%M:%S"),
            last_utc.strftime("%Y-%m-%d %H:%M:%S"),
            optimal_utc.strftime("%Y-%m-%d %H:%M:%S"),
            60,
            60,
            1,
            "backfill_agent2",
        ))
        inserted += 1

    conn.commit()
    print(f"  Inserted/replaced {inserted} days into market_history")

    if not high_temp_hours:
        conn.close()
        return

    # Calculate city_stats
    total      = inserted
    avg_high   = sum(high_temp_hours) / len(high_temp_hours)
    avg_final  = sum(iem_final_hours) / len(iem_final_hours)
    opt_hour   = avg_high + 1.0
    confidence = min(total / 100, 1.0) * 0.4 + 0.6
    now_str    = datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S")

    cur.execute("""
        INSERT OR REPLACE INTO city_stats
            (city, total_markets, avg_high_temp_hour, avg_iem_final_hour,
             avg_market_resolution_hour, optimal_entry_hour, success_rate,
             confidence_score, timezone, last_updated)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    """, (
        city,
        total,
        round(avg_high, 2),
        round(avg_final, 2),
        round(avg_final, 2),
        round(opt_hour, 2),
        1.0,
        round(confidence, 4),
        tz_str,
        now_str,
    ))
    conn.commit()
    conn.close()
    print(f"  city_stats updated: avg_high_temp_hour={avg_high:.2f}, optimal_entry_hour={opt_hour:.2f}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def main():
    print("=" * 60)
    print("backfill_agent2.py — IEM ASOS historical peak-hour backfill")
    print(f"Date range: {START_DATE.date()} to {END_DATE.date()} (365 days)")
    print("=" * 60)

    results = []   # (city, avg_high_hour, opt_hour) for summary table

    for idx, (city, station, tz_str, db_path) in enumerate(CITIES):
        process_city(city, station, tz_str, db_path, START_DATE, END_DATE)

        # Read back the city_stats we just wrote for the summary
        conn = sqlite3.connect(db_path)
        cur  = conn.cursor()
        row  = cur.execute(
            "SELECT avg_high_temp_hour, optimal_entry_hour, total_markets FROM city_stats WHERE city=?",
            (city,)
        ).fetchone()
        conn.close()
        if row:
            results.append((city, row[0], row[1], row[2]))
        else:
            results.append((city, None, None, 0))

        # Rate-limit: 0.5s sleep between stations (skip after last)
        if idx < len(CITIES) - 1:
            time.sleep(0.5)

    # Summary table
    print("\n")
    print("=" * 60)
    print("SUMMARY — city_stats after backfill")
    print("=" * 60)
    print(f"{'City':<12} {'Days':>5}  {'Avg High Hour':>14}  {'Optimal Entry Hour':>18}")
    print("-" * 60)
    for city, avg_h, opt_h, total in results:
        avg_h_str = f"{avg_h:.2f}" if avg_h is not None else "N/A"
        opt_h_str = f"{opt_h:.2f}" if opt_h is not None else "N/A"

        def fmt_hour(h):
            if h is None:
                return "N/A"
            hour = int(h)
            minute = int((h - hour) * 60)
            period = "AM" if hour < 12 else "PM"
            display = hour if hour <= 12 else hour - 12
            display = 12 if display == 0 else display
            return f"{avg_h_str} ({display}:{minute:02d} {period})"

        print(f"{city:<12} {total:>5}  {fmt_hour(avg_h):>30}  {opt_h_str:>18}")

    print("=" * 60)
    print("Done.")


if __name__ == "__main__":
    main()
