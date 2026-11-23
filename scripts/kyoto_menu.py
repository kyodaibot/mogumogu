# /// script
# requires-python = ">=3.10"
# dependencies = ["beautifulsoup4>=4.12,<5"]
# ///
"""Print the public Kyoto menu directory or one cafeteria as JSON."""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "importer"))
from menu_listing import main

if __name__ == "__main__":
    try:
        main()
    except ValueError as exc:
        sys.exit(str(exc))
