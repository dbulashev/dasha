#!/usr/bin/env python3
"""Static checks for the i18n locale files.

Catches what neither the bundler nor eslint catches: a key added to one locale
and forgotten in the others, a duplicate key silently overriding an earlier one,
and a translation whose {placeholders} drifted from the reference text.

ru_RU is the reference: it is the locale the features are written in first.

Findings come in two grades. Errors are defects in the files themselves —
invalid JSON, a duplicate key whose later value silently wins, a placeholder set
that drifted from the reference — and they fail the check. Warnings are gaps in
translation coverage (a key missing from a locale, or one that no longer exists
in the reference); those fall back at runtime, so they never fail unless
--strict is passed.
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
                        help='also fail on translation-coverage warnings')
    args = parser.parse_args()

    files = sorted(LOCALES_DIR.glob('*.json'))
    if not files:
        print(f'no locale files found in {LOCALES_DIR}')
        return 0

    errors = []
    warnings = []
    locales = {}

    for path in files:
        name = path.stem
        try:
            data, duplicates = load(path)
        except json.JSONDecodeError as err:
            errors.append(f'{name}: invalid JSON — {err}')
            continue

        for key in sorted(set(duplicates)):
            errors.append(f'{name}: duplicate key "{key}" — the later value silently wins')

        locales[name] = flatten(data)

    if REFERENCE not in locales:
        print(f'{REFERENCE}.json is missing or unparsable — nothing to compare against')
        return 1

    reference = locales[REFERENCE]
    for key, value in reference.items():
        if not isinstance(value, str):
            errors.append(f'{REFERENCE}: "{key}" is {type(value).__name__}, expected a string')

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
                errors.append(
                    f'{name}: "{key}" placeholders {sorted(actual)} != {REFERENCE} {sorted(expected)}')

    for line in errors:
        print(f'ERROR   {line}')
    for line in warnings:
        print(f'warning {line}')

    if errors or warnings:
        print(f'\nlocales: {len(errors)} error(s), {len(warnings)} warning(s) '
              f'in {len(locales)} files, {len(reference)} keys')
    else:
        print(f'locales OK: {len(locales)} files, {len(reference)} keys')

    if errors:
        return 1
    return 1 if (warnings and args.strict) else 0


if __name__ == '__main__':
    sys.exit(main())
