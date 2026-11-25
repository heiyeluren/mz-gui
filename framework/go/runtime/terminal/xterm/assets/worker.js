let terminal, addon, pending;
let replies=[];
const reply = (id,value) => process.stdout.write(JSON.stringify({id,...value})+'\n');
// 受控解析器没有异步插件；同步消费避免每个短 PTY 帧增加一次定时器延迟。
const write = data => terminal._core.writeSync(data);
let chain=Promise.resolve();
channel.on('line',line=>{chain=chain.then(async()=>{let id=0;try{
  if(line.length>3000000)throw Error('oversized request');
  const r=JSON.parse(line);id=r.id;replies=[];
  switch(r.op){
    case 'init': terminal=new Terminal({cols:r.cols,rows:r.rows,scrollback:10000,allowProposedApi:true});addon=new SerializeAddon();terminal.loadAddon(addon);terminalState.attach(terminal);pending=terminalState.pendingTracker();
      terminalState.setDefaultColors(terminal,r.colors);
      terminal.onData(data=>replies.push(data));
      for(const code of [10,11,12]) terminal.parser.registerOscHandler(code,data=>{if(data!=='?')return false;replies.push('\x1b]'+code+';rgb:'+terminalState.colorReply(terminal,code)+'\x1b\\');return true});
      break;
    case 'colors': terminalState.setDefaultColors(terminal,r.colors);break;
    case 'write': pending.feed(r.data);await write(r.data);break;
    case 'resize': terminal.resize(r.cols,r.rows);break;
    case 'restore': await new Promise((res,rej)=>terminalState.restore(terminal,r.state,e=>e?rej(e):res()));pending.reset(r.state.pending);break;
    case 'snapshot': {const state=terminalState.capture(terminal,addon,pending.get());terminalState.validate(state);reply(id,{state});return}
    default: throw Error('unknown operation');
  }
  reply(id,{ok:true,replies:replies.join('')});
}catch{reply(id,{error:'TERMINAL_STATE_FAILURE'})}})});
channel.on('close',()=>{chain.finally(()=>process.exit(0))});
reply(0,{ok:true});
