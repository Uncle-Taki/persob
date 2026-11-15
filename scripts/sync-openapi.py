from pathlib import Path
root=Path(__file__).resolve().parents[1]
(root/'internal/api/openapi.yaml').write_bytes((root/'docs/openapi.yaml').read_bytes())
