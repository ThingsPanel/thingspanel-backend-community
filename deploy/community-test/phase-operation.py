import hashlib,json,pathlib,re
path=pathlib.Path('/etc/nginx/conf.d/community.conf')
out={'path':str(path),'exists':path.is_file()}
if path.is_file():
    raw=path.read_bytes();text=raw.decode('utf-8')
    out['sha256']=hashlib.sha256(raw).hexdigest()
    out['server_name_and_root']=[line.strip() for line in text.splitlines() if re.match(r'^\s*(server_name|root)\s',line)]
    out['location_headers']=[line.strip() for line in text.splitlines() if re.match(r'^\s*location\s',line)]
    blocks=[]
    pattern=r'(?m)^\s*location\s+(?:=\s+|\^~\s+)?/(?:index\.html|mp-device/index\.html|SOURCE_COMMIT|assets/)?\s*\{(.*?)^\s*\}'
    for match in re.finditer(pattern,text,re.S):
        lines=match.group(0).splitlines()
        safe=[line.strip() for line in lines if re.match(r'^\s*(location|root|alias|try_files|index|expires|default_type|error_page)\b',line) or re.match(r'^\s*add_header\s+Cache-Control\b',line) or line.strip()=='}']
        directives=[re.match(r'^\s*([\w-]+)',line).group(1) for line in lines[1:-1] if re.match(r'^\s*([\w-]+)',line) and not line.strip().startswith('#')]
        blocks.append({'static_lines':safe,'directive_names':directives})
    out['static_blocks']=blocks
target=pathlib.Path('/home/thingspanel/nginx/www/dist')
if target.exists():
    out['site_path']=str(target.resolve())
    out['source_commit']=(target/'SOURCE_COMMIT').read_text().strip()
    out['site_index_sha256']=hashlib.sha256((target/'index.html').read_bytes()).hexdigest()
    out['profile_sha256']=hashlib.sha256((target/'RELEASE_PROFILE.json').read_bytes()).hexdigest()
print(json.dumps(out))
