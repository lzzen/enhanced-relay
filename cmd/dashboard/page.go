package main

// pageTemplate is a self-contained HTML page. The /*__DATA__*/ marker is
// replaced with a JSON object {acceptance, traceability, mutation}.
const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Enhanced Relay — Acceptance Dashboard</title>
<style>
  :root{--bg:#0d1117;--panel:#161b22;--border:#30363d;--fg:#e6edf3;--muted:#8b949e;
        --green:#3fb950;--red:#f85149;--amber:#d29922;--blue:#58a6ff}
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--fg);
       font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
  header{padding:20px 28px;border-bottom:1px solid var(--border);display:flex;
         align-items:center;gap:16px;flex-wrap:wrap}
  h1{font-size:18px;margin:0;font-weight:600}
  .badge{padding:4px 12px;border-radius:999px;font-weight:700;font-size:13px}
  .badge.pass{background:rgba(63,185,80,.15);color:var(--green);border:1px solid var(--green)}
  .badge.fail{background:rgba(248,81,73,.15);color:var(--red);border:1px solid var(--red)}
  .meta{color:var(--muted);font-size:12px;margin-left:auto;text-align:right}
  main{padding:24px 28px;max-width:1100px;margin:0 auto}
  .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px;margin-bottom:28px}
  .card{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:16px 18px}
  .card .label{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.04em}
  .card .value{font-size:28px;font-weight:700;margin-top:6px}
  .card .sub{color:var(--muted);font-size:12px;margin-top:4px}
  section{margin-bottom:32px}
  h2{font-size:15px;border-bottom:1px solid var(--border);padding-bottom:8px}
  table{width:100%;border-collapse:collapse;font-size:13px}
  th,td{text-align:left;padding:8px 10px;border-bottom:1px solid var(--border);vertical-align:top}
  th{color:var(--muted);font-weight:600}
  .pill{font-size:11px;padding:1px 8px;border-radius:999px;font-weight:600}
  .ok{color:var(--green)} .bad{color:var(--red)} .warn{color:var(--amber)}
  .p0{background:rgba(248,81,73,.15);color:var(--red)}
  .p1{background:rgba(210,153,34,.15);color:var(--amber)}
  .p2{background:rgba(139,148,158,.15);color:var(--muted)}
  .tests{color:var(--muted);font-size:12px}
  .bar{height:8px;border-radius:4px;background:#21262d;overflow:hidden;margin-top:6px}
  .bar>span{display:block;height:100%;background:var(--green)}
  .toolbar{margin:10px 0}
  button{background:var(--panel);color:var(--fg);border:1px solid var(--border);
         border-radius:6px;padding:6px 12px;cursor:pointer}
  button:hover{border-color:var(--blue)}
  code{background:#21262d;padding:1px 5px;border-radius:4px}
</style>
</head>
<body>
<header>
  <h1>Enhanced Relay — Acceptance Dashboard</h1>
  <span id="verdict" class="badge">…</span>
  <div class="meta" id="meta"></div>
</header>
<main>
  <div class="cards" id="cards"></div>
  <section>
    <h2>Requirement traceability</h2>
    <div class="toolbar"><button id="toggle">Show only unmet</button></div>
    <table id="reqTable"><thead><tr>
      <th>Requirement</th><th>Priority</th><th>Status</th><th>Bound tests</th>
    </tr></thead><tbody></tbody></table>
  </section>
  <section id="mutSection">
    <h2>Mutation testing (anti-gaming)</h2>
    <table id="mutTable"><thead><tr>
      <th>Package</th><th>Efficacy</th><th>Killed</th><th>Lived</th><th>Not covered</th><th>Mutator cov.</th>
    </tr></thead><tbody></tbody></table>
  </section>
</main>
<script>
const DATA = /*__DATA__*/{};
const a = DATA.acceptance || {};
const mut = DATA.mutation || null;

function el(tag, cls, html){const e=document.createElement(tag); if(cls)e.className=cls; if(html!=null)e.innerHTML=html; return e;}

// verdict
const overallPass = a.pass && (!mut || mut.pass !== false);
const v = document.getElementById('verdict');
v.textContent = overallPass ? 'PASS' : 'FAIL';
v.className = 'badge ' + (overallPass ? 'pass' : 'fail');
document.getElementById('meta').innerHTML =
  'commit <code>'+(a.commit||'?')+'</code> · '+(a.generated_at||'')+'<br>race: '+(a.race?'on':'off');

// cards
const t = a.tests||{}; const r = a.requirements||{};
const cards = [
  {label:'Tests', value:(t.passed||0)+' passed', sub:(t.failed||0)+' failed · '+(t.skipped||0)+' skipped', bad:(t.failed||0)>0},
  {label:'Requirements', value:(r.satisfied||0)+'/'+(r.total||0), sub:(r.missing_p0p1&&r.missing_p0p1.length? r.missing_p0p1.length+' P0/P1 unmet':'all P0/P1 met'), bad:!!(r.missing_p0p1&&r.missing_p0p1.length)},
];
if(mut){
  let min=100; (mut.packages||[]).forEach(p=>{min=Math.min(min,p.efficacy)});
  cards.push({label:'Mutation efficacy (min)', value:min.toFixed(0)+'%', sub:'threshold '+(mut.threshold||0)+'%', bad:mut.pass===false});
}
const cc = document.getElementById('cards');
cards.forEach(c=>{
  const d = el('div','card');
  d.appendChild(el('div','label',c.label));
  const val = el('div','value',c.value); if(c.bad) val.classList.add('bad'); else val.classList.add('ok');
  d.appendChild(val);
  d.appendChild(el('div','sub',c.sub));
  cc.appendChild(d);
});

// requirements table
let onlyUnmet=false;
function renderReqs(){
  const tb=document.querySelector('#reqTable tbody'); tb.innerHTML='';
  (r.details||[]).filter(x=>!onlyUnmet || !x.satisfied).forEach(x=>{
    const tr=el('tr');
    tr.appendChild(el('td','','<code>'+x.id+'</code><br><span class="tests">'+(x.desc||'')+'</span>'));
    const pr=(x.priority||'').toLowerCase();
    tr.appendChild(el('td','','<span class="pill '+pr+'">'+(x.priority||'')+'</span>'));
    tr.appendChild(el('td', x.satisfied?'ok':'bad', x.satisfied?'✓ met':'✗ unmet'));
    tr.appendChild(el('td','tests',(x.tests||[]).join('<br>')||'—'));
    tb.appendChild(tr);
  });
}
renderReqs();
document.getElementById('toggle').onclick=function(){onlyUnmet=!onlyUnmet; this.textContent=onlyUnmet?'Show all':'Show only unmet'; renderReqs();};

// mutation table
if(mut){
  const tb=document.querySelector('#mutTable tbody');
  (mut.packages||[]).forEach(p=>{
    const tr=el('tr');
    tr.appendChild(el('td','','<code>'+p.package+'</code>'));
    const good=p.efficacy>=(mut.threshold||0);
    tr.appendChild(el('td', good?'ok':'bad', p.efficacy.toFixed(0)+'%<div class="bar"><span style="width:'+p.efficacy+'%"></span></div>'));
    tr.appendChild(el('td','',p.killed));
    tr.appendChild(el('td', p.lived>0?'warn':'', p.lived));
    tr.appendChild(el('td','',p.notCovered));
    tr.appendChild(el('td','',(p.mcover!=null?p.mcover.toFixed(0)+'%':'—')));
    tb.appendChild(tr);
  });
}else{
  document.getElementById('mutSection').innerHTML='<h2>Mutation testing (anti-gaming)</h2><p class="tests">Not run in this pass. Run <code>make ci</code> / <code>make ci-docker</code> to include mutation results.</p>';
}
</script>
</body>
</html>`
