"""Real Chromium renders generated production bundle with synthetic read-only API fixtures."""
from pathlib import Path
import json, threading
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
from functools import partial
from playwright.sync_api import sync_playwright
root=Path(__file__).resolve().parents[2]
out=root/'mini-app/qa/results';out.mkdir(exist_ok=True)
class Handler(SimpleHTTPRequestHandler):
 def do_GET(self):
  if not self.path.startswith('/assets/'): self.path='/index.html'
  return super().do_GET()
 def log_message(self,*args):pass
server=ThreadingHTTPServer(('127.0.0.1',0),partial(Handler,directory=str(root/'internal/web/static')))
threading.Thread(target=server.serve_forever,daemon=True).start()
rows=[]
for i,(name,status,last,reply) in enumerate([
 ('Fixture success','replied','successful',True),('Fixture refusal','replied','rejected',True),('Fixture partial','replied','partially_successful',True),('Fixture unrated','replied','waiting_classification',True),('Fixture pending','pending','waiting_classification',False),('Fixture bounced','bounced','error_message',True),('Fixture unknown','replied','future_status',True)]):
 rows.append(dict(id=str(i),recipientName=name,subject=name,date='2099-01-01T10:00:00Z',status=status,lastStatus=last,replyReceivedAt='2099-01-02T10:00:00Z' if reply else '',delivered=True,channel='dostup'))
api={'/api/requests':rows,'/api/me':{'firstName':'Тест'},'/api/portal-stats':{'requests':100,'successful':40,'organs':20},'/api/rating':{'items':[{'name':'Fixture authority','requests':100,'successful':40,'overdue':7,'slug':'fixture'}]},'/api/analytics':{'total':7,'answered':5,'ackOnly':1,'awaiting':1,'bounced':1}}
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
 page=browser.new_page(viewport={'width':390,'height':844},device_scale_factor=1)
 errors=[]; page.on('pageerror',lambda err: errors.append(str(err)))
 def route(r):
  key=r.request.url.split('/api/',1)[1].split('?',1)[0]
  r.fulfill(json={'ok':True,'data':api.get('/api/'+key,{})})
 page.route('**/api/**',route)
 page.route('https://telegram.org/**',lambda r:r.fulfill(body=''))
 origin=f'http://127.0.0.1:{server.server_port}'
 page.goto(origin+'/requests');page.get_by_text('Fixture refusal',exact=True).first.wait_for()
 assert page.get_by_text('Відповідь без оцінки',exact=True).count()==2
 for label,expected in [('Відмови',['Fixture refusal']),('Частково',['Fixture partial']),('Відповіді',['Fixture success','Fixture refusal','Fixture partial','Fixture unrated','Fixture unknown']),('Очікують',['Fixture pending'])]:
  page.get_by_role('button',name=label,exact=True).click()
  body=page.locator('body').inner_text()
  for row in rows: assert (row['subject'] in body)==(row['subject'] in expected),(label,row['subject'],body)
 page.get_by_role('button',name='Усі',exact=True).click()
 page.screenshot(path=str(out/'requests-mobile.png'),full_page=True)
 page.goto(origin+'/rating');page.get_by_text('Fixture authority',exact=True).wait_for()
 body=page.locator('body').inner_text();assert 'Успішних: 40' in body and 'Відповідей:' not in body and 'За даними порталу' in body
 page.screenshot(path=str(out/'rating-mobile.png'),full_page=True)
 assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
 page.goto(origin+'/analytics');page.get_by_text('Зведені дані журналу',exact=False).wait_for()
 page.screenshot(path=str(out/'analytics-mobile.png'),full_page=True)
 page.goto(origin+'/');page.get_by_text('Остання відповідь',exact=True).wait_for()
 latest=page.get_by_text('Остання відповідь',exact=True).locator('xpath=../..')
 assert 'Fixture bounced' not in latest.inner_text()
 assert page.get_by_text('Відповідей',exact=True).count()>=1
 page.screenshot(path=str(out/'overview-mobile.png'),full_page=True)
 assert not errors,errors
 (out/'report.json').write_text(json.dumps({'passed':True,'browser':'Chromium','viewport':'390x844','fixtures':len(rows),'checks':['refusal filter','partial filter','all receipts filter','unrated receipt badge','classification without receipt pending','bounced excluded from replies','portal metric labels','local analytics scope','no ranking horizontal overflow','no page errors']},indent=2))
 browser.close()
server.shutdown();print('UI fixtures passed; screenshots saved:',out)
