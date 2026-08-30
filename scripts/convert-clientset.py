#!/usr/bin/env python3
"""One-off refactor: route every Kubernetes API call through Client.cs().

Reading the c.Clientset field directly races with Bind, which swaps the live
cluster at runtime. Each method must instead take one snapshot and use it.

Run from backend/internal/kubernetes. Reports any method it cannot convert
safely so those can be handled by hand.
"""
import glob
import re
import sys

HEAD = re.compile(r'^func \(c \*Client\) (\w+)\(')

# Guard variables are named csErr rather than err so the insert can never
# collide with an err the method already declares.
GUARD_HEAD = '\tcs, csErr := c.cs()'


def split_signature(line):
    """Returns (name, return-signature) for a one-line method header."""
    m = HEAD.match(line)
    if not m:
        return None, None
    depth = 0
    for i in range(m.end() - 1, len(line)):
        if line[i] == '(':
            depth += 1
        elif line[i] == ')':
            depth -= 1
            if depth == 0:
                rest = line[i + 1:].strip()
                if not rest.endswith('{'):
                    return None, None
                return m.group(1), rest[:-1].strip()
    return None, None


def zero_values(ret):
    """Zero values to return alongside the error, or None if not derivable."""
    if ret == 'error':
        return []
    if not (ret.startswith('(') and ret.endswith(')')):
        return None
    inner = ret[1:-1]
    # Split on commas at depth 0 so map[string]string survives intact.
    parts, depth, cur = [], 0, ''
    for ch in inner:
        if ch in '([{':
            depth += 1
        elif ch in ')]}':
            depth -= 1
        if ch == ',' and depth == 0:
            parts.append(cur.strip())
            cur = ''
        else:
            cur += ch
    parts.append(cur.strip())

    if not parts or not parts[-1].split()[-1] == 'error':
        return None

    out = []
    for p in parts[:-1]:
        t = p.split()[-1] if ' ' in p else p
        if t.startswith('*') or t.startswith('[]') or t.startswith('map['):
            out.append('nil')
        elif t == 'string':
            out.append('""')
        elif t == 'bool':
            out.append('false')
        elif t in ('int', 'int32', 'int64', 'float64'):
            out.append('0')
        elif re.fullmatch(r'[\w.]+', t):
            out.append(t + '{}')
        else:
            return None
    return out


# Signatures with no error to return: fall back to a zero value. These are
# best-effort helpers whose callers already treat an empty result as "unknown".
NO_ERROR = {
    'string': 'return ""',
    'bool': 'return false',
    '[]NamespaceResource': 'return nil',
    'map[string]nodeLoad': 'return nil',
    'map[string]string': 'return nil',
    '*WorkloadServiceInfo': 'return nil',
    '': 'return',
}

# Handled by hand: these are the accessor's own siblings.
SKIP = {'streamClientset', 'cs', 'restConfig', 'Available', 'Bind'}

converted, skipped = 0, []

for path in sorted(glob.glob('*.go')):
    if path.endswith('_test.go'):
        continue
    lines = open(path).read().split('\n')

    heads = []
    for i, line in enumerate(lines):
        name, ret = split_signature(line)
        if name:
            heads.append((i, name, ret))
    if not heads:
        continue

    # Rewrite from the bottom so earlier line numbers stay valid.
    for idx in range(len(heads) - 1, -1, -1):
        i, name, ret = heads[idx]
        end = heads[idx + 1][0] if idx + 1 < len(heads) else len(lines)
        body = lines[i + 1:end]

        if name in SKIP or not any('c.Clientset' in l for l in body):
            continue

        zeros = zero_values(ret)
        if zeros is not None:
            ret_stmt = '\t\treturn ' + ', '.join(zeros + ['csErr'])
        elif ret in NO_ERROR:
            ret_stmt = '\t\t' + NO_ERROR[ret]
        else:
            skipped.append(f'{path}:{name} -> {ret!r}')
            continue

        guard = [GUARD_HEAD, '\tif csErr != nil {', ret_stmt, '\t}']
        lines[i + 1:end] = guard + [l.replace('c.Clientset', 'cs') for l in body]
        converted += 1

    open(path, 'w').write('\n'.join(lines))

print(f'converted {converted} methods')
if skipped:
    print('MANUAL:')
    for s in skipped:
        print(' ', s)
sys.exit(0)
