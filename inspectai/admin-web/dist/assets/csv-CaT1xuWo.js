function b(e,o,s){const r=c=>{const n=String(c??"");return/[",\n]/.test(n)?`"${n.replace(/"/g,'""')}"`:n},a=[o,...s].map(c=>c.map(r).join(",")),p=new Blob(["\uFEFF"+a.join(`\r
`)],{type:"text/csv;charset=utf-8"}),t=document.createElement("a");t.href=URL.createObjectURL(p),t.download=/\.csv$/.test(e)?e:`${e}.csv`,t.click(),URL.revokeObjectURL(t.href)}export{b as e};
