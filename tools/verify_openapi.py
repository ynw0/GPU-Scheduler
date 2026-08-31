from pathlib import Path

import yaml


SPEC_PATH = Path("api/openapi.yaml")
METHODS = {"get", "post", "put", "patch", "delete"}
PATCH_PATHS = {
    "/compose-sources/{id}",
    "/templates/{id}",
    "/users/{id}",
    "/quotas/{id}",
    "/notifications/{id}",
}


def resolve_local_ref(spec: dict, reference: str) -> None:
    if not reference.startswith("#/"):
        raise AssertionError(f"unsupported external reference: {reference}")
    current = spec
    for encoded_part in reference[2:].split("/"):
        part = encoded_part.replace("~1", "/").replace("~0", "~")
        if not isinstance(current, dict) or part not in current:
            raise AssertionError(f"unresolved reference: {reference}")
        current = current[part]


def verify_references(spec: dict, value: object) -> None:
    if isinstance(value, dict):
        reference = value.get("$ref")
        if isinstance(reference, str):
            resolve_local_ref(spec, reference)
        for child in value.values():
            verify_references(spec, child)
    elif isinstance(value, list):
        for child in value:
            verify_references(spec, child)


def main() -> None:
    spec = yaml.safe_load(SPEC_PATH.read_text(encoding="utf-8"))
    assert spec["openapi"] == "3.1.0"
    operations = [
        operation
        for path in spec["paths"].values()
        for method, operation in path.items()
        if method in METHODS
    ]
    assert len(operations) == 36, len(operations)
    operation_ids = [operation["operationId"] for operation in operations]
    assert len(operation_ids) == len(set(operation_ids)), "duplicate operationId"
    for path in PATCH_PATHS:
        assert "patch" in spec["paths"][path], path
    verify_references(spec, spec)


if __name__ == "__main__":
    main()
