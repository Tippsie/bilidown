import sqlite3

with sqlite3.connect("data.db") as db:
    rows = db.execute("""
        SELECT id, create_at, content
        FROM log
        ORDER BY id DESC
        LIMIT 20
    """)

    for row_id, created, message in rows:
        print(f"[{created}] #{row_id}: {message}")
