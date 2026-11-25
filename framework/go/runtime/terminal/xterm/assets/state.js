// xterm 6.0.0 状态桥。私有字段集中在本文件，升级必须跑同流恢复后继续写入的差分测试。
export const terminalState = (() => {
  const observed = new WeakMap();
  const defaults = new WeakMap();
  const defaultRGB = {10:'d8d8/dddd/e2e2',11:'1818/1c1c/2020',12:'6969/b7b7/aaaa'};
  const validColor = value => /^(?:#[0-9a-fA-F]{3,12}|rgb:[0-9a-fA-F/]{3,16})$/.test(value);
  function attach(t) {
    if(observed.has(t))return;
    const colors={};observed.set(t,colors);
    t.parser.registerOscHandler(4,data=>{const parts=data.split(';');for(let i=0;i+1<parts.length;i+=2){const n=+parts[i];if(Number.isInteger(n)&&n>=0&&n<256&&validColor(parts[i+1]))colors['p'+n]=parts[i+1]}return false});
    t.parser.registerOscHandler(104,data=>{if(!data){for(const key of Object.keys(colors))if(key[0]==='p')delete colors[key]}else for(const n of data.split(';'))delete colors['p'+n];return false});
    for(const code of [10,11,12]) {
      t.parser.registerOscHandler(code,data=>{if(validColor(data))colors['s'+code]=data;return false});
      t.parser.registerOscHandler(code+100,()=>{delete colors['s'+code];return false});
    }
  }
  function setDefaultColors(t,colors={}) {
    const value={};
    for(const key of ['foreground','background','cursor']) {
      if(colors[key]===undefined)continue;
      if(!/^#[0-9a-fA-F]{6}$/.test(colors[key]))throw Error('invalid default color');
      value[key]=colors[key];
    }
    defaults.set(t,value);
    // 浏览器恢复与后台应答使用同一主题；headless 没有 theme 选项。
    if(t.options.theme!==undefined)t.options.theme={...t.options.theme,...value};
  }
  function colorReply(t,code){const color=observed.get(t)?.['s'+code]||defaults.get(t)?.[{10:'foreground',11:'background',12:'cursor'}[code]];if(color?.startsWith('rgb:'))return color.slice(4).split('/').map(v=>(v.repeat(4)).slice(0,4)).join('/');if(color?.startsWith('#')){const h=color.slice(1),n=h.length/3;if(Number.isInteger(n))return [0,1,2].map(i=>h.slice(i*n,(i+1)*n).repeat(4).slice(0,4)).join('/')}return defaultRGB[code]}
  function extendedCells(b,c,retained) {
    const cells=[],links={};
    for(let y=Math.max(0,b.ybase-retained);y<b.lines.length;y++) {
      const line=b.lines.get(y);
      for(const key of Object.keys(line._extendedAttrs)) {const x=+key,v=line._extendedAttrs[key];if(x>=line.length)continue;const id=v.urlId||0;if(id)links[id]=c._oscLinkService.getLinkData(id);cells.push([y-b.ybase,x,line._data[x*3+1],line._data[x*3+2],v._ext,id])}
    }
    return {cells,links};
  }
  function applyExtended(b,c,s) {
    const active=c._bufferService.buffers._activeBuffer;c._bufferService.buffers._activeBuffer=b;
    try {const ids={};for(const [id,data] of Object.entries(s.links)){ids[id]=c._oscLinkService.registerLink(data)}
      for(const [dy,x,fg,bg,ext,id] of s.cells){const y=b.ybase+dy;if(y<0||y>=b.lines.length||x<0||x>=b._cols)continue;const line=b.lines.get(y),value=c._inputHandler._curAttrData.extended.clone();value._ext=ext;value.urlId=ids[id]||0;line._extendedAttrs[x]=value;line._data[x*3+1]=fg;line._data[x*3+2]=bg;if(value.urlId)c._oscLinkService.addLineToLink(value.urlId,y)}
    }finally{c._bufferService.buffers._activeBuffer=active}
  }
  const modeKeys = ['applicationCursorKeys','applicationKeypad','bracketedPasteMode','cursorBlink','cursorStyle','origin','reverseWraparound','sendFocus','wraparound','synchronizedOutput'];
  const clone = value => JSON.parse(JSON.stringify(value));
  const attr = (value, core) => ({ fg:value.fg, bg:value.bg, extended:clone(value.extended), link: value.extended.urlId ? core._oscLinkService.getLinkData(value.extended.urlId) : null });
  function bufferState(b,c) { return {x:b.x,y:b.y,savedX:b.savedX,savedY:b.savedY-b.ybase,scrollTop:b.scrollTop,scrollBottom:b.scrollBottom,tabs:clone(b.tabs),savedCurAttrData:attr(b.savedCurAttrData,c),savedCharset:b.savedCharset}; }
  function applyAttr(to, value, c) { to.fg=value.fg;to.bg=value.bg;Object.assign(to.extended,value.extended);to.extended.urlId=value.link ? c._oscLinkService.registerLink(value.link) : 0; }
  function applyBuffer(b,v,c,cols,rows) { b.x=Math.max(0,Math.min(cols,v.x));b.y=Math.max(0,Math.min(rows-1,v.y));b.savedX=v.savedX;b.savedY=b.ybase+v.savedY;b.scrollTop=v.scrollTop;b.scrollBottom=v.scrollBottom;b.tabs=Object.assign(Object.create(null),v.tabs);applyAttr(b.savedCurAttrData,v.savedCurAttrData,c);b.savedCharset=v.savedCharset; }
  class StateBudgetError extends Error {}
  function captureAt(t,a,pending,retained) {
    const c=t._core;const ansi=a.serialize({excludeModes:true,scrollback:retained});
    const modes={};for(const key of modeKeys)modes[key]=c.coreService.decPrivateModes[key];
    const result={defaultColors:clone(defaults.get(t)||{}),historyTruncated:c._bufferService.buffers.normal.ybase>retained,palette:clone(observed.get(t)||{}),normalExtended:extendedCells(c._bufferService.buffers.normal,c,retained),alternateExtended:extendedCells(c._bufferService.buffers.alt,c,0),version:1,xterm:'6.0.0',cols:t.cols,rows:t.rows,ansi,pending,
      normal:bufferState(c._bufferService.buffers.normal,c),alternate:bufferState(c._bufferService.buffers.alt,c),modes,
      insertMode:c.coreService.modes.insertMode,cursorHidden:c.coreService.isCursorHidden,
      cursorInitialized:c.coreService.isCursorInitialized,
      attributes:attr(c._inputHandler._curAttrData,c),charsets:clone(c._charsetService._charsets),glevel:c._charsetService.glevel,
      mouseProtocol:c.coreMouseService.activeProtocol,mouseEncoding:c.coreMouseService.activeEncoding,
      precedingJoinState:c._inputHandler._parser.precedingJoinState,
      title:c._inputHandler._windowTitle,icon:c._inputHandler._iconName,
      titleStack:clone(c._inputHandler._windowTitleStack),iconStack:clone(c._inputHandler._iconNameStack)};
    if(new TextEncoder().encode(JSON.stringify(result)).length>2000000)throw new StateBudgetError('screen state exceeds bound');
    return result;
  }
  function capture(t,a,pending='') {
    for(const retained of [10000,1000,100,0]){try{return captureAt(t,a,pending,retained)}catch(error){if(!(error instanceof StateBudgetError)||retained===0)throw error}}
    throw new StateBudgetError('screen state exceeds bound');
  }
  function validate(s) {
    const integer = n => Number.isSafeInteger(n);
    const object = v => v && typeof v==='object' && !Array.isArray(v);
    const charset = v => v==null || (object(v) && Object.keys(v).length<=256 && Object.entries(v).every(([k,val])=>k.length===1 && typeof val==='string' && val.length<=16));
    const validAttr = v => object(v)&&integer(v.fg)&&integer(v.bg)&&object(v.extended)&&integer(v.extended._ext)&&integer(v.extended._urlId)&&(v.link==null||(object(v.link)&&typeof v.link.uri==='string'&&(v.link.id===undefined||typeof v.link.id==='string')));
    if(!s||s.version!==1||s.xterm!=='6.0.0'||!integer(s.cols)||s.cols<2||s.cols>500||!integer(s.rows)||s.rows<2||s.rows>200||typeof s.ansi!=='string'||typeof s.pending!=='string'||new TextEncoder().encode(JSON.stringify(s)).length>2000000)throw Error('invalid terminal state');
    if(s.defaultColors!==undefined&&(!object(s.defaultColors)||!Object.entries(s.defaultColors).every(([key,value])=>['foreground','background','cursor'].includes(key)&&typeof value==='string'&&/^#[0-9a-fA-F]{6}$/.test(value))))throw Error('invalid default colors');
    if(!object(s.modes)||!validAttr(s.attributes)||!Array.isArray(s.charsets)||s.charsets.length>4||!s.charsets.every(charset)||!integer(s.glevel)||s.glevel<0||s.glevel>3||!integer(s.precedingJoinState))throw Error('invalid terminal modes');
    for(const key of modeKeys){const v=s.modes[key];if(key==='cursorStyle'){if(v!==undefined&&!['block','bar','underline'].includes(v))throw Error('invalid cursor style')}else if(v!==undefined&&typeof v!=='boolean')throw Error('invalid terminal mode')}
    for(const b of [s.normal,s.alternate]) {
      if(!b||!['x','y','savedX','savedY','scrollTop','scrollBottom'].every(k=>integer(b[k]))||b.scrollTop<0||b.scrollBottom>=s.rows||b.scrollTop>b.scrollBottom||!object(b.tabs)||!Object.entries(b.tabs).every(([k,v])=>/^\d+$/.test(k)&&+k<500&&typeof v==='boolean')||!validAttr(b.savedCurAttrData)||!charset(b.savedCharset))throw Error('invalid terminal buffer state');
    }
    if(!object(s.palette)||!Object.entries(s.palette).every(([k,v])=>(/^p\d+$/.test(k)&&+k.slice(1)<256||/^s1[012]$/.test(k))&&typeof v==='string'&&validColor(v)))throw Error('invalid palette');
    for(const extended of [s.normalExtended,s.alternateExtended]) {
      if(!extended||!Array.isArray(extended.cells)||!object(extended.links)||!extended.cells.every(cell=>Array.isArray(cell)&&cell.length===6&&cell.every(integer))||!Object.values(extended.links).every(link=>object(link)&&typeof link.uri==='string'&&(link.id===undefined||typeof link.id==='string')))throw Error('invalid extended cells');
    }
    if(!['NONE','X10','VT200','DRAG','ANY'].includes(s.mouseProtocol)||!['DEFAULT','SGR','SGR_PIXELS'].includes(s.mouseEncoding)||typeof s.title!=='string'||typeof s.icon!=='string'||!Array.isArray(s.titleStack)||!Array.isArray(s.iconStack)||!s.titleStack.every(x=>typeof x==='string')||!s.iconStack.every(x=>typeof x==='string'))throw Error('invalid terminal metadata');
    const pending=pendingTracker();pending.feed(s.pending);if(pending.get()!==s.pending)throw Error('invalid parser continuation');
  }
  function restore(t,s,done) {
    validate(s);t.reset();t.resize(s.cols,s.rows);
    if(s.defaultColors!==undefined)setDefaultColors(t,s.defaultColors);
    attach(t);Object.keys(observed.get(t)).forEach(k=>delete observed.get(t)[k]);Object.assign(observed.get(t),s.palette);
    let palette='';for(const [key,color] of Object.entries(s.palette)){if(validColor(color))palette+='\x1b]'+(key[0]==='p'?'4;'+key.slice(1):key.slice(1))+';'+color+'\x07'}
    t.write('\x1b[?2026h'+palette+s.ansi,()=>{try {
      const c=t._core;
      applyBuffer(c._bufferService.buffers.normal,s.normal,c,s.cols,s.rows);
      applyBuffer(c._bufferService.buffers.alt,s.alternate,c,s.cols,s.rows);
      applyExtended(c._bufferService.buffers.normal,c,s.normalExtended);applyExtended(c._bufferService.buffers.alt,c,s.alternateExtended);
      for(const k of modeKeys) c.coreService.decPrivateModes[k]=s.modes[k];
      c.coreService.modes.insertMode=!!s.insertMode;c.coreService.isCursorHidden=!!s.cursorHidden;c.coreService.isCursorInitialized=!!s.cursorInitialized;
      applyAttr(c._inputHandler._curAttrData,s.attributes,c);
      c._charsetService._charsets=s.charsets;c._charsetService.setgLevel(s.glevel);
      c.coreMouseService.activeProtocol=s.mouseProtocol;c.coreMouseService.activeEncoding=s.mouseEncoding;

      c._inputHandler._windowTitle=s.title;c._inputHandler._iconName=s.icon;c._inputHandler._windowTitleStack=s.titleStack;c._inputHandler._iconNameStack=s.iconStack;
      // 保留解析到一半的控制序列，下一增量可以从原位置继续解析。
      t.write(s.modes.synchronizedOutput?'':'\x1b[?2026l',()=>{c._inputHandler._parser.precedingJoinState=s.precedingJoinState;t.write(s.pending,()=>{if(t.refresh&&!s.modes.synchronizedOutput)t.refresh(0,t.rows-1);done()})});
    }catch(err){done(err)}});
  }
  // 捕获尚未完成的控制序列；不解释或执行数据，也不向 PTY 发送应答。
  function pendingTracker() {
    let state=0,raw='',stringState=0;
    return {feed(data){for(const ch of data){const n=ch.charCodeAt(0);
      if(n===24||n===26){state=0;raw='';continue}
      if(state===0){if(n===27){state=1;raw=ch}else if(n===155){state=2;raw=ch}else if([144,152,157,158,159].includes(n)){state=n===157?3:4;raw=ch}continue}
      raw+=ch;if(raw.length>1048576)throw Error('terminal control sequence exceeds bound');
      if(state===1){if(ch==='[')state=2;else if(ch===']')state=3;else if(['P','X','^','_'].includes(ch))state=4;else if(n>=32&&n<=47)state=5;else if(n>=48&&n<=126){state=0;raw=''}}
      else if(state===2){if(n>=64&&n<=126){state=0;raw=''}else if(n===27){state=1;raw=ch}}
      else if(state===3||state===4){if(n===156||(state===3&&n===7)){state=0;raw=''}else if(n===27){stringState=state;state=6}}
      else if(state===5){if(n>=48&&n<=126){state=0;raw=''}else if(n===27){state=1;raw=ch}}
      else if(state===6){if(ch==='\\'){state=0;raw=''}else state=stringState}
    }},get(){return raw},reset(prefix=''){state=0;raw='';this.feed(prefix)}};
  }
  return {capture,restore,validate,pendingTracker,attach,colorReply,setDefaultColors,defaultTheme:{background:'#181c20',foreground:'#d8dde2',cursor:'#69b7aa'}};
})();
