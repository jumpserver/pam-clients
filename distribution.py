"""Build the same versioned PAM downloads for Core images and GitHub releases."""

import argparse
import ast
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent
PLATFORMS = ('linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64', 'windows/arm64')
GO_SOURCES = ('go/*.go', 'go/go.mod', 'go/go.sum')
SOURCE_FILES = {
    'socket': ('socket/python_example.py', 'socket/README.*.md'),
    'python': ('python/jms_pam/**/*.py', 'python/jms_pam/py.typed', 'python/setup.py',
               'python/pyproject.toml', 'python/MANIFEST.in', 'python/LICENSE', 'python/README.*.md',
               'python/subclass_demo.py', 'python/demo.py', 'python/application_info.py', 'python/list_accounts.py'),
    'go': GO_SOURCES,
    'java': ('java/src/main/**/*.java', 'java/pom.xml'),
    'node': ('node/index.js', 'node/index.d.ts', 'node/event-dispatcher.js', 'node/demo.js',
             'node/accounts.js', 'node/application-info.js', 'node/events.js', 'node/hooks.js',
             'node/README.*.md', 'node/package.json', 'node/package-lock.json'),
    'curl': ('curl/demo.sh', 'curl/README.*.md'),
    'agent': GO_SOURCES + ('agent/cmd/**/*.go', 'agent/internal/**/*.go', 'agent/go.mod', 'agent/go.sum'),
}


def client_version(root=ROOT):
    tree = ast.parse((root / 'python/jms_pam/_version.py').read_text(encoding='utf-8'))
    for node in tree.body:
        if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == '__version__' for t in node.targets):
            value = ast.literal_eval(node.value)
            if isinstance(value, str) and re.fullmatch(r'\d+\.\d+\.\d+(?:(?:a|b|rc)\d+)?', value):
                return value
    raise ValueError('Invalid PAM client version')


def source_files(language, root=ROOT):
    files = {}
    for pattern in SOURCE_FILES[language]:
        for path in root.glob(pattern):
            relative = path.relative_to(root)
            if (not path.is_file() or path.is_symlink() or path.name.endswith('_test.go')
                    or any((root / parent).is_symlink() for parent in relative.parents)):
                continue
            files[relative.as_posix()] = path
    return files


def source_archive(language, root=ROOT):
    files = source_files(language, root)
    if not files:
        raise ValueError(f'No source files for {language}')
    output = io.BytesIO()
    with gzip.GzipFile(filename='', fileobj=output, mode='wb', mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as archive:
            for name, path in sorted(files.items()):
                data = path.read_bytes()
                item = tarfile.TarInfo(f'jms-pam-clients/{name}')
                item.size = len(data)
                item.mode = 0o755 if name.endswith('.sh') else 0o644
                archive.addfile(item, io.BytesIO(data))
    return output.getvalue()


def python_sdk_zip(root=ROOT):
    output = io.BytesIO()
    with zipfile.ZipFile(output, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for name, path in sorted(source_files('python', root).items()):
            item = zipfile.ZipInfo(name.removeprefix('python/'))
            item.compress_type = zipfile.ZIP_DEFLATED
            item.external_attr = 0o644 << 16
            archive.writestr(item, path.read_bytes())
    return output.getvalue()


def verify_versions(root, version):
    declarations = {
        'go/auth.go': rf'const Version = "{re.escape(version)}"',
        'node/index.js': rf"const VERSION = '{re.escape(version)}'",
        'java/src/main/java/org/jumpserver/pam/Client.java': rf'final String VERSION = "{re.escape(version)}"',
        'java/pom.xml': rf'<artifactId>jms-pam</artifactId><version>{re.escape(version)}</version>',
    }
    for path, pattern in declarations.items():
        if not re.search(pattern, (root / path).read_text(encoding='utf-8')):
            raise ValueError(f'{path} does not match Python version {version}')
    if json.loads((root / 'node/package.json').read_text())['version'] != version:
        raise ValueError('Node package version does not match Python')
    lock = json.loads((root / 'node/package-lock.json').read_text())
    if lock['version'] != version or lock['packages']['']['version'] != version:
        raise ValueError('Node lockfile version does not match Python')


def build(output, platforms=PLATFORMS, root=ROOT):
    version = client_version(root)
    verify_versions(root, version)
    if len(set(platforms)) != len(platforms) or any(p not in PLATFORMS for p in platforms):
        raise ValueError('Unsupported or duplicate Agent platform')
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    destination = output / version
    if destination.exists():
        raise FileExistsError(f'Refusing to overwrite {destination}; use a clean output directory')
    # Build in a private staging directory so a failed build cannot publish partial downloads.
    with tempfile.TemporaryDirectory(prefix='.pam-build-', dir=output) as temporary:
        staging = Path(temporary) / version
        staging.mkdir()
        for language in SOURCE_FILES:
            (staging / f'jms-pam-{language}-source.tar.gz').write_bytes(source_archive(language, root))
        (staging / 'jms-pam-python.zip').write_bytes(python_sdk_zip(root))
        for platform in platforms:
            system, architecture = platform.split('/')
            suffix = '.exe' if system == 'windows' else ''
            binary = staging / f'pam-agent-{system}-{architecture}{suffix}'
            environment = {**os.environ, 'CGO_ENABLED': '0', 'GOOS': system, 'GOARCH': architecture, 'GOWORK': 'off'}
            subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w',
                            '-o', str(binary), './cmd/pam-agent'], cwd=root / 'agent', env=environment, check=True)
        subprocess.run([sys.executable, '-m', 'build', '--no-isolation', '--outdir', str(staging), str(root / 'python')], check=True)
        hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(staging.iterdir()) if p.is_file()}
        metadata = {'version': version, 'agent_platforms': list(platforms), 'sha256': hashes}
        (staging / 'manifest.json').write_text(json.dumps(metadata, indent=2) + '\n', encoding='utf-8')
        hashes['manifest.json'] = hashlib.sha256((staging / 'manifest.json').read_bytes()).hexdigest()
        (staging / 'SHA256SUMS').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(hashes.items())), encoding='utf-8')
        shutil.move(str(staging), destination)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--platforms', default=','.join(PLATFORMS), help='Comma-separated Agent OS/architecture pairs')
    args = parser.parse_args()
    print(build(args.output, tuple(args.platforms.split(','))))


if __name__ == '__main__':
    main()
