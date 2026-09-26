import re
src=open('src/concilia.html',encoding='utf-8').read()
s=src
s=re.sub(r'<link rel="preconnect"[^>]*>\n','',s)
s=re.sub(r'<link rel="stylesheet" href="https://fonts.googleapis.com[^>]*>\n','<link rel="stylesheet" href="fonts.css">\n',s)
s=s.replace('<script src="https://cdnjs.cloudflare.com/ajax/libs/xlsx/0.18.5/xlsx.full.min.js"></script>','<script src="lib/xlsx.full.min.js"></script>')
s=s.replace("const base='https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/';","const base='lib/';")
s=s.replace("Los archivos se leen en tu navegador. No se suben ni se guardan en ningún servidor.","Los archivos se procesan en este equipo. No se suben ni se envían a ningún servidor.")
s=s.replace("const EXCELJS_SRC='https://cdnjs.cloudflare.com/ajax/libs/exceljs/4.4.0/exceljs.min.js';","const EXCELJS_SRC='lib/exceljs.min.js';")
assert 'cdnjs' not in s and 'googleapis' not in s
head='<!doctype html>\n<html lang="es">\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width,initial-scale=1">\n<style>:root{color-scheme:light}body{margin:0}img{max-width:100%}[hidden]{display:none!important}</style>\n'
open('web/index.html','w',encoding='utf-8').write(head+s+'\n</html>\n')
print('ok')
