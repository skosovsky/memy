#!/usr/bin/env python3
"""Run optional semantic consumer fixtures in an isolated temporary Go module."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

MODULES = [
    ('github.com/skosovsky/memy', 'memy'),
    ('github.com/skosovsky/ragy', 'ragy'),
    ('github.com/skosovsky/contexty', 'contexty'),
    ('github.com/skosovsky/toolsy', 'toolsy'),
    ('github.com/skosovsky/toolsy/toolkits/memory', 'toolsy/toolkits/memory'),
    ('github.com/skosovsky/toolsy/toolkits/rag', 'toolsy/toolkits/rag'),
]


def run(args, cwd, env, capture=False):
    return subprocess.run(args, cwd=cwd, env=env, check=True, text=True,
                          stdout=subprocess.PIPE if capture else None).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('lane', choices=['local', 'published'])
    parser.add_argument('--memy-ref', help='Exact published tag for the changed API')
    parser.add_argument('--siblings', type=Path, help='Optional local consumer checkout directory')
    parser.add_argument('--manifest', type=Path, help='Write resolved module manifest')
    args = parser.parse_args()
    if args.lane == 'published' and (not args.memy_ref or not args.memy_ref.startswith('v')):
        parser.error('published lane requires an exact --memy-ref tag')
    root = Path(__file__).resolve().parent.parent
    env = dict(os.environ, GOWORK='off')
    # A dedicated cache can be supplied by CI or the caller; no checkout mutation.
    env.setdefault('GOCACHE', '/tmp/memy-go-build')
    env['GOMODCACHE'] = env.get('MEMY_CONSUMER_MODCACHE', '/tmp/memy-consumer-mod')
    env['GOPATH'] = env.get('MEMY_CONSUMER_GOPATH', '/tmp/memy-consumer-gopath')
    manifest = {'lane': args.lane, 'modules': [], 'work': 'off'}
    with tempfile.TemporaryDirectory(prefix='memy-consumer-') as temp:
        workspace = Path(temp)
        module = workspace / 'consumer'
        module.mkdir()
        for fixture in (root / 'testdata' / 'consumer').glob('*.go'):
            shutil.copy2(fixture, module / fixture.name)
        toolchain = next(line for line in (root / 'go.mod').read_text().splitlines() if line.startswith('go '))
        (module / 'go.mod').write_text('module consumer.local/managed\n\n' + toolchain + '\n')
        checkouts = {'memy': root}
        for name, rel in MODULES:
            if args.lane == 'local':
                repo = rel.split('/')[0]
                if repo not in checkouts:
                    existing = args.siblings / repo if args.siblings else None
                    if existing and (existing / 'go.mod').is_file():
                        checkouts[repo] = existing.resolve()
                    else:
                        destination = workspace / repo
                        run(['git', 'clone', '--depth=1', 'https://github.com/skosovsky/' + repo + '.git', str(destination)], workspace, env)
                        checkouts[repo] = destination
                source = checkouts[repo].joinpath(*rel.split('/')[1:])
                run(['go', 'mod', 'edit', '-require=' + name + '@v0.0.0', '-replace=' + name + '=' + str(source)], module, env)
                head = run(['git', 'rev-parse', 'HEAD'], source, env, True).strip()
                files = sorted(p for p in source.rglob('*') if p.is_file() and p.suffix == '.go' and '.git' not in p.parts)
                digest = hashlib.sha256()
                for path in files:
                    digest.update(str(path.relative_to(source)).encode() + b'\0' + path.read_bytes())
                manifest['modules'].append({'module': name, 'source_head': head, 'go_source_sha256': digest.hexdigest(), 'local': True})
            else:
                ref = args.memy_ref if rel == 'memy' else 'latest'
                info = json.loads(run(['go', 'list', '-m', '-json', name + '@' + ref], module, env, True))
                version = info['Version']
                run(['go', 'mod', 'edit', '-require=' + name + '@' + version], module, env)
                manifest['modules'].append({'module': name, 'version': version, 'local': False})
        run(['go', 'mod', 'tidy'], module, env)
        if args.lane == 'published':
            graph = json.loads('[' + ','.join(_json_objects(run(['go', 'list', '-m', '-json', 'all'], module, env, True))) + ']')
            if any(item.get('Replace') for item in graph):
                raise RuntimeError('Published consumer graph contains a replace directive')
            manifest['resolved_graph'] = graph
        run(['go', 'test', '-mod=readonly', '-race', '-count=1', '-v', './...'], module, env)
        manifest['result'] = 'pass'
    if args.manifest:
        args.manifest.parent.mkdir(parents=True, exist_ok=True)
        args.manifest.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(manifest, ensure_ascii=False))


def _json_objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        value, length = decoder.raw_decode(text.lstrip())
        yield json.dumps(value)
        text = text.lstrip()[length:]


if __name__ == '__main__':
    main()
