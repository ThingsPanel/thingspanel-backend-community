import subprocess,json,pathlib,tempfile,sys,os
listener='import http.server,json,time,datetime,sys,pathlib\nout={\'at\':datetime.datetime.now().isoformat(),\'requests\':[]};device=\'373fb5d9-a73a-e948-dcb5-5ab07aed6dec\';deadline=time.time()+100\nclass Handler(http.server.BaseHTTPRequestHandler):\n def setup(self):\n  http.server.BaseHTTPRequestHandler.setup(self);self.connection.settimeout(2)\n def log_message(self,*args):pass\n def do_POST(self):\n  try:length=int(self.headers.get(\'Content-Length\',\'0\'))\n  except (TypeError,ValueError):self.send_error(400);return\n  if length<0:self.send_error(400);return\n  if length>10000:self.send_error(413);return\n  try:body=json.loads(self.rfile.read(length))\n  except Exception:self.send_error(400);return\n  if self.path!=\'/api/v1/control\' or body.get(\'tp_device_number\')!=device:self.send_error(404);return\n  out[\'requests\'].append({\'path\':self.path,\'body\':body});pathlib.Path(sys.argv[2]).write_text(json.dumps(out));self.send_response(200);self.end_headers();self.wfile.write(b\'{"ok":true}\')\nwith http.server.HTTPServer((sys.argv[1],35180),Handler) as s:\n s.timeout=1\n while time.time()<deadline:s.handle_request()\nout[\'stopped\']=True;pathlib.Path(sys.argv[2]).write_text(json.dumps(out))\n'
ids=subprocess.check_output(['docker','ps','-q']).decode().split();gateway=None
for cid in ids:
 x=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
 for n,v in x['NetworkSettings']['Networks'].items():
  if 'tp-plugin-http-adapter' in (v.get('Aliases') or []) and v.get('Gateway'):gateway=v['Gateway']
if not gateway:raise RuntimeError('HTTP plugin bridge gateway not found; refusing public bind')
d=pathlib.Path(tempfile.mkdtemp(prefix='qa-q06-http-direct-'));os.chmod(str(d),0o700);p=d/'listener.py';p.write_text(listener);result=d/'result.json';log=(d/'server.log').open('wb')
child=subprocess.Popen([sys.executable,str(p),gateway,str(result)],stdin=subprocess.DEVNULL,stdout=log,stderr=log,start_new_session=True,close_fds=True)
print(json.dumps({'pid':child.pid,'host':gateway,'port':35180,'deadline_seconds':100,'result_path':str(result),'log_path':str(d/'server.log')}))
