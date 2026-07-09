import{j as u}from"./index-D-2m1I_S.js";import{r as c}from"./antd-BSzgR-7F.js";import{C as h,D as b,t as R,E as l,n as f,B as E}from"./animation-DSLRwPFy.js";/**
 * Anime.js - utils - ESM
 * @version v4.5.0
 * @license MIT
 * @copyright 2026 - Julian Garnier
 */const _=(t,n)=>(+t).toFixed(n),x=(t,n,e)=>`${t}`.padStart(n,e),y=(t,n,e)=>`${t}`.padEnd(n,e),P=(t,n,e)=>((t-n)%(e-n)+(e-n))%(e-n)+n,S=(t,n,e,r,s)=>r+(t-n)/(e-n)*(s-r),T=t=>t*Math.PI/180,j=t=>t*180/Math.PI,C=(t,n,e,r)=>r?r===1?n:l(t,n,1-Math.exp(-r*e*.1)):t,D=Object.freeze(Object.defineProperty({__proto__:null,clamp:b,damp:C,degToRad:T,lerp:l,mapRange:S,padEnd:y,padStart:x,radToDeg:j,round:R,roundPad:_,snap:h,wrap:P},Symbol.toStringTag,{value:"Module"}));/**
 * Anime.js - utils - ESM
 * @version v4.5.0
 * @license MIT
 * @copyright 2026 - Julian Garnier
 */const a=D,p={},M=(t,n=0)=>(...e)=>n?r=>t(...e,r):r=>t(r,...e),i=t=>(...n)=>{const e=t(...n);return new Proxy(f,{apply:(r,s,[d])=>e(d),get:(r,s)=>{if(p[s])return i((...d)=>{const m=p[s](...d);return g=>m(e(g))})}})},o=(t,n,e=0)=>{const r=(...s)=>(s.length<n.length?i(M(n,e)):n)(...s);return p[t]||(p[t]=r),r};o("roundPad",a.roundPad);o("padStart",a.padStart);o("padEnd",a.padEnd);o("wrap",a.wrap);o("mapRange",a.mapRange);o("degToRad",a.degToRad);o("radToDeg",a.radToDeg);o("snap",a.snap);o("clamp",a.clamp);const v=o("round",a.round);o("lerp",a.lerp,1);o("damp",a.damp,1);function F({value:t}){const[n,e]=c.useState(0),r=c.useRef(0);return c.useEffect(()=>{const s={v:r.current},d=E(s,{v:t,modifier:v(0),duration:900,ease:"outCubic",onUpdate:()=>e(s.v)});return r.current=t,()=>{d.pause()}},[t]),u.jsx(u.Fragment,{children:n})}export{F as C};
