"""Descarga las tipografías IBM Plex (paquetes @fontsource de npm) a web/fonts
y genera web/fonts.css para que el programa funcione sin conexión.
Se ejecuta en la compilación automática; localmente requiere Node/npm."""
import os, re, subprocess, tarfile, tempfile, shutil, glob

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, 'web', 'fonts')
PKGS = {'ibm-plex-sans': [400, 500, 600, 700], 'ibm-plex-mono': [400, 500, 600]}
VERSION = '5.3.0'

def main():
    if os.path.isdir(OUT):
        shutil.rmtree(OUT)
    os.makedirs(OUT)
    faces = []
    with tempfile.TemporaryDirectory() as tmp:
        for pkg, weights in PKGS.items():
            subprocess.run(['npm', 'pack', f'@fontsource/{pkg}@{VERSION}', '--silent'], cwd=tmp, check=True, shell=(os.name == 'nt'))
            tgz = glob.glob(os.path.join(tmp, f'fontsource-{pkg}-*.tgz'))[0]
            dest = os.path.join(tmp, pkg)
            with tarfile.open(tgz) as t:
                t.extractall(dest)
            base = os.path.join(dest, 'package')
            for w in weights:
                css = open(os.path.join(base, f'{w}.css'), encoding='utf-8').read()
                for block in re.findall(r'@font-face\s*{[^}]*}', css):
                        if not re.search(r'-latin(-ext)?-\d+-normal\.woff2', block):
                            continue
                        for url in re.findall(r'url\(\.?/?files/([^)]+?\.woff2)\)', block):
                            shutil.copy(os.path.join(base, 'files', url), os.path.join(OUT, url))
                        block = re.sub(r'src:[^;]*;', lambda m: 'src: url(fonts/' + re.search(r'files/([^)]+?\.woff2)', m.group(0)).group(1) + ") format('woff2');", block)
                        faces.append(block)
    with open(os.path.join(ROOT, 'web', 'fonts.css'), 'w', encoding='utf-8') as f:
        f.write('\n'.join(faces) + '\n')
    print(f'{len(faces)} tipografías en web/fonts')

if __name__ == '__main__':
    main()
