import json,socket,struct,time,threading,subprocess,pathlib,re
out={'scope':'real Modbus TCP listener on QA-only port 35102; no platform MQTT publisher','frames':[],'processes':[]}
regs=[200,0];deadline=time.time()+150
def handle(c):
 c.settimeout(15)
 def read(n):
  b=b''
  while len(b)<n:
   x=c.recv(n-len(b))
   if not x:raise EOFError()
   b+=x
  return b
 try:
  while time.time()<deadline:
   h=read(7);tid,proto,length,unit=struct.unpack('!HHHB',h);pdu=read(length-1);fn=pdu[0]
   out['frames'].append({'unit':unit,'transaction':tid,'function':fn,'pdu_hex':pdu.hex()})
   if fn==3:
    addr,count=struct.unpack('!HH',pdu[1:5]);vals=[regs[i] if i<len(regs) else 0 for i in range(addr,addr+count)]
    reply=bytes([fn,count*2])+b''.join(struct.pack('!H',v) for v in vals)
   elif fn==6:
    addr,val=struct.unpack('!HH',pdu[1:5]);
    if addr<len(regs):regs[addr]=val
    reply=pdu
   else:reply=bytes([fn|128,1])
   c.sendall(struct.pack('!HHHB',tid,0,len(reply)+1,unit)+reply)
 except (EOFError,socket.timeout,ConnectionError):pass
 finally:c.close()
with socket.socket() as s:
 s.bind(('127.0.0.1',35102));s.listen(5);s.settimeout(1)
 while time.time()<deadline:
  try:c,a=s.accept();threading.Thread(target=handle,args=(c,),daemon=True).start()
  except socket.timeout:pass
out['final_registers']=regs
import urllib.request
try:
 out['monitor']=json.load(urllib.request.urlopen('http://127.0.0.1:1506/api/monitor/stats',timeout=5))
except Exception as e:out['monitor_error']=type(e).__name__
print(json.dumps(out))
