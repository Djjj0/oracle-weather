#!/usr/bin/env python3
"""
backfill_agent1.py - Backfill 365 days of historical weather peak-hour data
into learning_international.db for the oracle weather trading bot.

Fetches hourly IEM ASOS data for 6 international cities, finds the daily
high temperature and the local hour it occurred, then inserts into market_history
and updates city_stats.
"""

import sqlite3
import time
import requests
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo
from io import StringIO

# ── Configuration ──────────────────────────────────────────────────────────────

CITIES = [
    {"name": "London",       "station": "EGLC", "timezone": "Europe/London"},
    {"name": "Paris",        "station": "LFPG", "timezone": "Europe/Paris"},
    {"name": "Seoul",        "station": "RKSI", "timezone": "Asia/Seoul"},
    {"name": "Toronto",      "station": "CYYZ", "timezone": "America/Toronto"},
    {"name": "Buenos Aires", "station": "SAEZ", "timezone": "America/Argentina/Buenos_Aires"},
    {"name": "Lucknow",      "station": "VILK", "timezone": "Asia/Kolkata"},
]

DB_PATH = "data/learning_international.db"

# Fetch from 2025-03-07 through 2026-03-06 (365 days)
START_DATE = datetime(2025, 3, 7)
END_DATE   = datetime(2026, 3, 6)

IEM_BASE = "https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py"

SLEEP_BETWEEN_STATIONS = 0.5   # seconds — polite rate limit
REQUEST_TIMEOUT        = 120   # seconds


# ── IEM fetch ──────────────────────────────────────────────────────────────────

def fetch_iem_hourly(station: str, start: datetime, end: datetime) -> list[dict]:
    """
    Fetch hourly tmpf observations from IEM ASOS for the given station and
    date range (inclusive).  Returns a list of dicts:
        {"utc_dt": datetime (UTC-aware), "tmpf": float}
    Rows with null/missing temps are silently skipped.
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
        "report_type": [3, 4],   # requests sends repeated keys as list
    }

    print(f"  Fetching IEM data for {station} ...", end=" ", flush=True)
    try:
        resp = requests.get(IEM_BASE, params=params, timeout=REQUEST_TIMEOUT)
        resp.raise_for_status()
    except requests.RequestException as exc:
        print(f"ERROR: {exc}")
        return []

    lines = resp.text.splitlines()
    # First two lines are a header comment and the column-name row
    # station,valid,tmpf
    observations = []
    for line in lines:
        line = line.strip()
        if not line or line.startswith("station") or line.startswith("#"):
            continue
        parts = line.split(",")
        if len(parts) < 3:
            continue
        valid_str = parts[1].strip()
        tmpf_str  = parts[2].strip()
        if tmpf_str.lower() in ("null", "m", "trace", ""):
            continue
        try:
            tmpf = float(tmpf_str)
        except ValueError:
            continue
        try:
            # IEM UTC timestamps look like "2025-03-07 14:53"
            utc_dt = datetime.strptime(valid_str, "%Y-%m-%d %H:%M").replace(tzinfo=timezone.utc)
        except ValueError:
            continue
        observations.append({"utc_dt": utc_dt, "tmpf": tmpf})

    print(f"{len(observations)} observations received.")
    return observations


# ── Daily aggregation ──────────────────────────────────────────────────────────

def group_by_local_date(observations: list[dict], tz_str: str) -> dict:
    """
    Convert each UTC observation to the local timezone and group by local
    calendar date.  Returns:
        { date_str: [{"local_dt": datetime, "utc_dt": datetime, "tmpf": float}, ...] }
    """
    local_tz = ZoneInfo(tz_str)
    by_date: dict[str, list] = {}
    for obs in observations:
        local_dt = obs["utc_dt"].astimezone(local_tz)
        date_str = local_dt.strftime("%Y-%m-%d")
        by_date.setdefault(date_str, []).append(
            {"local_dt": local_dt, "utc_dt": obs["utc_dt"], "tmpf": obs["tmpf"]}
        )
    return by_date


def fahrenheit_to_celsius(f: float) -> float:
    return round((f - 32) * 5 / 9, 2)


def build_daily_records(city: dict, observations: list[dict]) -> list[dict]:
    """
    For each local calendar day within [START_DATE, END_DATE], find the
    observation with the highest temperature and build the row dict for
    market_history.
    """
    tz_str   = city["timezone"]
    city_name = city["name"]
    by_date  = group_by_local_date(observations, tz_str)

    records = []
    current = START_DATE
    while current <= END_DATE:
        date_str = current.strftime("%Y-%m-%d")
        current += timedelta(days=1)

        day_obs = by_date.get(date_str)
        if not day_obs:
            # No data for this day — skip silently
            continue

        # Find observation with maximum temperature
        peak = max(day_obs, key=lambda x: x["tmpf"])
        # Last observation of the day (latest UTC timestamp)
        last_obs = max(day_obs, key=lambda x: x["utc_dt"])

        high_temp_c        = fahrenheit_to_celsius(peak["tmpf"])
        high_temp_time_utc = peak["utc_dt"]
        iem_final_utc      = last_obs["utc_dt"]
        optimal_entry_utc  = high_temp_time_utc + timedelta(hours=1)

        market_id = f"{city_name}_{date_str}"

        records.append({
            "market_id":             market_id,
            "city":                  city_name,
            "date":                  date_str,
            "timezone":              tz_str,
            "high_temp":             high_temp_c,
            "high_temp_time":        high_temp_time_utc.strftime("%Y-%m-%d %H:%M:%S"),
            "iem_data_final_time":   iem_final_utc.strftime("%Y-%m-%d %H:%M:%S"),
            "market_resolved_time":  iem_final_utc.strftime("%Y-%m-%d %H:%M:%S"),
            "optimal_entry_time":    optimal_entry_utc.strftime("%Y-%m-%d %H:%M:%S"),
            "data_lag_minutes":      60,
            "resolution_lag_minutes": 60,
            "success":               1,
            "notes":                 "backfill_agent1",
        })

    return records


# ── Database writes ────────────────────────────────────────────────────────────

INSERT_HISTORY_SQL = """
INSERT OR REPLACE INTO market_history (
    market_id, city, date, timezone,
    high_temp, high_temp_time,
    iem_data_final_time, market_resolved_time,
    optimal_entry_time, data_lag_minutes,
    resolution_lag_minutes, success, notes
) VALUES (
    :market_id, :city, :date, :timezone,
    :high_temp, :high_temp_time,
    :iem_data_final_time, :market_resolved_time,
    :optimal_entry_time, :data_lag_minutes,
    :resolution_lag_minutes, :success, :notes
)
"""

UPSERT_CITY_STATS_SQL = """
INSERT OR REPLACE INTO city_stats (
    city, total_markets, avg_high_temp_hour, avg_iem_final_hour,
    avg_market_resolution_hour, optimal_entry_hour,
    success_rate, confidence_score, timezone, last_updated
) VALUES (
    :city, :total_markets, :avg_high_temp_hour, :avg_iem_final_hour,
    :avg_market_resolution_hour, :optimal_entry_hour,
    :success_rate, :confidence_score, :timezone, :last_updated
)
"""


def insert_records(conn: sqlite3.Connection, records: list[dict]):
    """Bulk-insert market_history rows using INSERT OR REPLACE."""
    conn.executemany(INSERT_HISTORY_SQL, records)
    conn.commit()
    print(f"    Inserted/replaced {len(records)} rows into market_history.")


def update_city_stats(conn: sqlite3.Connection, city: dict, records: list[dict]):
    """
    Recalculate city-level statistics from all rows for this city in
    market_history (not just the ones just inserted, so existing rows
    from earlier runs are included) and upsert city_stats.
    """
    city_name = city["name"]
    tz_str    = city["timezone"]
    local_tz  = ZoneInfo(tz_str)

    # Pull all rows for this city from the DB so we include any pre-existing data
    cur = conn.execute(
        "SELECT high_temp_time, iem_data_final_time, market_resolved_time FROM market_history WHERE city = ?",
        (city_name,),
    )
    rows = cur.fetchall()

    if not rows:
        print(f"    WARNING: No rows found for {city_name} after insert — skipping city_stats update.")
        return

    high_temp_hours       = []
    iem_final_hours       = []
    market_resolved_hours = []

    for (high_temp_time_str, iem_final_str, resolved_str) in rows:
        for time_str, bucket in [
            (high_temp_time_str, high_temp_hours),
            (iem_final_str,      iem_final_hours),
            (resolved_str,       market_resolved_hours),
        ]:
            if not time_str:
                continue
            try:
                utc_dt   = datetime.strptime(time_str, "%Y-%m-%d %H:%M:%S").replace(tzinfo=timezone.utc)
                local_dt = utc_dt.astimezone(local_tz)
                bucket.append(local_dt.hour + local_dt.minute / 60.0)
            except ValueError:
                continue

    def safe_avg(lst):
        return round(sum(lst) / len(lst), 4) if lst else 0.0

    total_markets       = len(rows)
    avg_high_temp_hour  = safe_avg(high_temp_hours)
    avg_iem_final_hour  = safe_avg(iem_final_hours)
    avg_resolved_hour   = safe_avg(market_resolved_hours)
    optimal_entry_hour  = round(avg_high_temp_hour + 1.0, 4)
    success_rate        = 1.0
    confidence_score    = round(min(total_markets / 100, 1.0) * 0.4 + 0.6, 4)

    stats = {
        "city":                      city_name,
        "total_markets":             total_markets,
        "avg_high_temp_hour":        avg_high_temp_hour,
        "avg_iem_final_hour":        avg_iem_final_hour,
        "avg_market_resolution_hour": avg_resolved_hour,
        "optimal_entry_hour":        optimal_entry_hour,
        "success_rate":              success_rate,
        "confidence_score":          confidence_score,
        "timezone":                  tz_str,
        "last_updated":              datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S"),
    }
    conn.execute(UPSERT_CITY_STATS_SQL, stats)
    conn.commit()
    print(
        f"    city_stats updated: total={total_markets}, "
        f"avg_high_hour={avg_high_temp_hour:.2f}, "
        f"optimal_entry_hour={optimal_entry_hour:.2f}, "
        f"confidence={confidence_score:.4f}"
    )


# ── Main ───────────────────────────────────────────────────────────────────────

def main():
    print("=" * 65)
    print("  backfill_agent1.py — Oracle Weather Bot Historical Backfill")
    print("=" * 65)
    print(f"  Date range : {START_DATE.strftime('%Y-%m-%d')} to {END_DATE.strftime('%Y-%m-%d')}")
    print(f"  Cities     : {len(CITIES)}")
    print(f"  Database   : {DB_PATH}")
    print("=" * 65)

    conn = sqlite3.connect(DB_PATH)
    conn.execute("PRAGMA journal_mode=WAL")   # safer concurrent writes

    summary_rows = []

    for idx, city in enumerate(CITIES, 1):
        print(f"\n[{idx}/{len(CITIES)}] {city['name']} (station={city['station']}, tz={city['timezone']})")

        # ── 1. Fetch ──────────────────────────────────────────────────────────
        observations = fetch_iem_hourly(city["station"], START_DATE, END_DATE)

        if not observations:
            print(f"  WARNING: No observations returned for {city['name']} — skipping.")
            summary_rows.append({
                "city":               city["name"],
                "days_inserted":      0,
                "avg_high_temp_hour": "N/A",
                "optimal_entry_hour": "N/A",
            })
            if idx < len(CITIES):
                time.sleep(SLEEP_BETWEEN_STATIONS)
            continue

        # ── 2. Build daily records ────────────────────────────────────────────
        records = build_daily_records(city, observations)
        print(f"  Days with data : {len(records)}")

        if len(records) < 100:
            print(f"  WARNING: Only {len(records)} days returned — data may be sparse for {city['name']}.")

        # ── 3. Insert into market_history ─────────────────────────────────────
        insert_records(conn, records)

        # ── 4. Update city_stats ──────────────────────────────────────────────
        update_city_stats(conn, city, records)

        # Read back what we just wrote for the summary
        cur = conn.execute(
            "SELECT avg_high_temp_hour, optimal_entry_hour FROM city_stats WHERE city = ?",
            (city["name"],),
        )
        row = cur.fetchone()
        summary_rows.append({
            "city":               city["name"],
            "days_inserted":      len(records),
            "avg_high_temp_hour": f"{row[0]:.2f}" if row else "N/A",
            "optimal_entry_hour": f"{row[1]:.2f}" if row else "N/A",
        })

        # Polite pause between stations
        if idx < len(CITIES):
            time.sleep(SLEEP_BETWEEN_STATIONS)

    conn.close()

    # ── Summary table ─────────────────────────────────────────────────────────
    print("\n")
    print("=" * 65)
    print("  BACKFILL COMPLETE — Summary")
    print("=" * 65)
    header = f"{'City':<18} {'Days':>6}  {'Avg High Hour':>14}  {'Optimal Entry':>14}"
    print(header)
    print("-" * 60)
    for r in summary_rows:
        # Convert decimal hour to HH:MM for readability
        def hour_to_hhmm(h_str):
            try:
                h = float(h_str)
                hh = int(h)
                mm = int((h - hh) * 60)
                return f"{hh:02d}:{mm:02d} (local)"
            except (ValueError, TypeError):
                return h_str

        print(
            f"{r['city']:<18} {r['days_inserted']:>6}  "
            f"{hour_to_hhmm(r['avg_high_temp_hour']):>22}  "
            f"{hour_to_hhmm(r['optimal_entry_hour']):>22}"
        )
    print("=" * 65)


if __name__ == "__main__":
    main()
