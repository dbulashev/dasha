#!/usr/bin/env python3
"""Static checks for the i18n locale files.

Catches what neither the bundler nor eslint catches: a key added to one locale
and forgotten in the others, a duplicate key silently overriding an earlier one,
and a translation whose {placeholders} drifted from the reference text.

ru_RU is the reference: it is the locale the features are written in first.
Findings are reported as warnings and the check exits 0 — untranslated strings
fall back at runtime, so they must not block a build. Use --strict to fail.
"""

import argparse
import json
import re
import sys
from pathlib import Path

LOCALES_DIR = Path(__file__).resolve().parent.parent / 'src' / 'locales'
REFERENCE = 'ru_RU'

PLACEHOLDER = re.compile(r'\{(\w+)\}')


def load(path):
    """Parse a locale file, reporting duplicate keys json.load would swallow."""
    duplicates = []

    def object_pairs_hook(pairs):
        seen = set()
        for key, _ in pairs:
            if key in seen:
                duplicates.append(key)
            seen.add(key)
        return dict(pairs)

    with path.open(encoding='utf-8') as fh:
        return json.load(fh, object_pairs_hook=object_pairs_hook), duplicates


def flatten(data, prefix=''):
    out = {}
    for key, value in data.items():
        if isinstance(value, dict):
            out.update(flatten(value, prefix + key + '.'))
        else:
            out[prefix + key] = value
    return out


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--strict', action='store_true',
                        help='exit 1 when anything is reported')
    args = parser.parse_args()

    files = sorted(LOCALES_DIR.glob('*.json'))
    if not files:
        print(f'no locale files found in {LOCALES_DIR}')
        return 0

    warnings = []
    locales = {}

    for path in files:
        name = path.stem
        try:
            data, duplicates = load(path)
        except json.JSONDecodeError as err:
            warnings.append(f'{name}: invalid JSON — {err}')
            continue

        for key in sorted(set(duplicates)):
            warnings.append(f'{name}: duplicate key "{key}" — the later value silently wins')

        locales[name] = flatten(data)

    if REFERENCE not in locales:
        print(f'{REFERENCE}.json is missing or unparsable — nothing to compare against')
        return 1 if args.strict else 0

    reference = locales[REFERENCE]
    for key, value in reference.items():
        if not isinstance(value, str):
            warnings.append(f'{REFERENCE}: "{key}" is {type(value).__name__}, expected a string')

    for name, flat in sorted(locales.items()):
        if name == REFERENCE:
            continue

        for key in sorted(set(reference) - set(flat)):
            warnings.append(f'{name}: missing key "{key}" (present in {REFERENCE})')
        for key in sorted(set(flat) - set(reference)):
            warnings.append(f'{name}: unknown key "{key}" (absent from {REFERENCE})')

        for key, value in flat.items():
            if key not in reference or not isinstance(value, str) or not isinstance(reference[key], str):
                continue
            expected = set(PLACEHOLDER.findall(reference[key]))
            actual = set(PLACEHOLDER.findall(value))
            if expected != actual:
                warnings.append(
                    f'{name}: "{key}" placeholders {sorted(actual)} != {REFERENCE} {sorted(expected)}')

    if warnings:
        print('\n'.join(warnings))
        print(f'\nlocales: {len(warnings)} warning(s) in {len(locales)} files, {len(reference)} keys')
        return 1 if args.strict else 0

    print(f'locales OK: {len(locales)} files, {len(reference)} keys')
    return 0


if __name__ == '__main__':
    sys.exit(main())
