from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
resource_text = "\n".join(p.read_text(encoding="utf-8") for p in (root / "android/app/src/main/res").rglob("*.xml"))
kotlin_text = "\n".join(p.read_text(encoding="utf-8") for p in (root / "android/app/src/main/java").rglob("*.kt"))
ids_in_layout = set(re.findall(r"@\+?id/([A-Za-z0-9_]+)", resource_text))
ids_in_code = set(re.findall(r"R\.id\.([A-Za-z0-9_]+)", kotlin_text))
missing = sorted(ids_in_code - ids_in_layout)
if missing:
    raise SystemExit("missing resource ids: " + ", ".join(missing))
print(f"resource_contract=ok ids={len(ids_in_code)}")
