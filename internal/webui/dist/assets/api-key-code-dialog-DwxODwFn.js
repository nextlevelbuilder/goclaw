import{s as e}from"./chunk-Bj-mKKzh.js";import{n as t,t as n}from"./jsx-runtime-Bs4MhNha.js";import{n as r}from"./createLucideIcon-_caY3eyu.js";import{t as i}from"./check-C0i5CTjH.js";import{t as a}from"./code-xml-xMyNLFAC.js";import{t as o}from"./copy-B3IiL8S7.js";import{a as s,n as c,o as l,r as u,t as d}from"./dialog-Bi4XKMR1.js";import{t as f}from"./button-Cl43W_B2.js";var p=e(t(),1),m=n();function h(e,t){return e.split(`
`).map((e,n)=>(0,m.jsx)(`div`,{children:g(e,t)||`\xA0`},n))}function g(e,t){if(t===`curl`&&e.trimStart().startsWith(`#`)||(t===`typescript`||t===`go`)&&e.trimStart().startsWith(`//`))return(0,m.jsx)(`span`,{className:`text-emerald-600 dark:text-emerald-400`,children:e});let n=[],r=e,i=0,a=t===`go`?/\b(package|import|func|var|const|defer|if|err|nil|map|string|any)\b/g:t===`typescript`?/\b(const|let|await|async|new|return|import|from|export)\b/g:null,o=/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g,s=0,c;for(;(c=o.exec(r))!==null;){if(c.index>s){let e=r.slice(s,c.index);n.push((0,m.jsx)(`span`,{children:_(e,a)},i++))}n.push((0,m.jsx)(`span`,{className:`text-amber-600 dark:text-amber-400`,children:c[0]},i++)),s=c.index+c[0].length}if(s<r.length){let e=r.slice(s);n.push((0,m.jsx)(`span`,{children:_(e,a)},i))}return n.length===0?e:(0,m.jsx)(m.Fragment,{children:n})}function _(e,t){if(!t||!e)return e;let n=[],r=0,i=0,a,o=new RegExp(t.source,`g`);for(;(a=o.exec(e))!==null;)a.index>r&&n.push(e.slice(r,a.index)),n.push((0,m.jsx)(`span`,{className:`text-violet-600 dark:text-violet-400 font-semibold`,children:a[0]},i++)),r=a.index+a[0].length;return r<e.length&&n.push(e.slice(r)),n.length===0?e:(0,m.jsx)(m.Fragment,{children:n})}function v({open:e,onOpenChange:t}){let{t:n}=r(`api-keys`),[g,_]=(0,p.useState)(`curl`),[v,S]=(0,p.useState)(!1),C=`https://YOUR-GOCLAW-BACKEND`,w=`YOUR-GOCLAW-API-KEY`,T=(0,p.useMemo)(()=>({curl:y(C,w,n),typescript:b(C,w,n),go:x(C,w,n)}),[C,w,n]),E=(0,p.useMemo)(()=>h(T[g],g),[T,g]),D=async()=>{await navigator.clipboard.writeText(T[g]),S(!0),setTimeout(()=>S(!1),2e3)},O=[{key:`curl`,label:n(`codeDialog.tabs.curl`)},{key:`typescript`,label:n(`codeDialog.tabs.typescript`)},{key:`go`,label:n(`codeDialog.tabs.go`)}];return(0,m.jsx)(d,{open:e,onOpenChange:t,children:(0,m.jsxs)(c,{className:`max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-2xl flex flex-col max-h-[85vh]`,children:[(0,m.jsxs)(s,{children:[(0,m.jsxs)(l,{className:`flex items-center gap-2`,children:[(0,m.jsx)(a,{className:`h-5 w-5`}),n(`codeDialog.title`)]}),(0,m.jsx)(u,{children:n(`codeDialog.description`)})]}),(0,m.jsxs)(`div`,{className:`flex items-center justify-between`,children:[(0,m.jsx)(`div`,{className:`flex rounded-lg bg-muted p-1 gap-0.5`,children:O.map(e=>(0,m.jsx)(`button`,{onClick:()=>{_(e.key),S(!1)},className:`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${g===e.key?`bg-background text-foreground shadow-sm`:`text-muted-foreground hover:text-foreground`}`,children:e.label},e.key))}),(0,m.jsxs)(f,{variant:`outline`,size:`sm`,onClick:D,className:`gap-1.5 h-8`,children:[v?(0,m.jsx)(i,{className:`h-3.5 w-3.5`}):(0,m.jsx)(o,{className:`h-3.5 w-3.5`}),(0,m.jsx)(`span`,{className:`text-xs`,children:n(v?`codeDialog.copied`:`codeDialog.copy`)})]})]}),(0,m.jsx)(`div`,{className:`relative rounded-lg bg-muted/60 border overflow-auto min-h-0 flex-1`,children:(0,m.jsx)(`pre`,{className:`p-4 text-xs leading-relaxed font-mono whitespace-pre`,children:E})})]})})}function y(e,t,n){return`# ${n(`codeDialog.comments.chat`)}
curl -X POST ${e}/v1/chat/completions \\
  -H "Authorization: Bearer ${t}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "agent": "your-agent-key",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# ${n(`codeDialog.comments.listAgents`)}
curl ${e}/v1/agents \\
  -H "Authorization: Bearer ${t}"

# ${n(`codeDialog.comments.listSessions`)}
curl ${e}/v1/sessions \\
  -H "Authorization: Bearer ${t}"`}function b(e,t,n){return`const BASE_URL = "${e}";
const API_KEY = "${t}";

// ${n(`codeDialog.comments.chat`)}
const res = await fetch(\`\${BASE_URL}/v1/chat/completions\`, {
  method: "POST",
  headers: {
    "Authorization": \`Bearer \${API_KEY}\`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    agent: "your-agent-key",
    messages: [{ role: "user", content: "Hello!" }],
  }),
});

const data = await res.json();
console.log(data.choices[0].message.content);

// ${n(`codeDialog.comments.listAgents`)}
const agents = await fetch(\`\${BASE_URL}/v1/agents\`, {
  headers: { "Authorization": \`Bearer \${API_KEY}\` },
}).then(r => r.json());`}function x(e,t,n){return`package main

import (
\t"bytes"
\t"encoding/json"
\t"fmt"
\t"io"
\t"net/http"
)

func main() {
\tbaseURL := "${e}"
\tapiKey := "${t}"

\t// ${n(`codeDialog.comments.chat`)}
\tbody, _ := json.Marshal(map[string]any{
\t\t"agent":    "your-agent-key",
\t\t"messages": []map[string]string{
\t\t\t{"role": "user", "content": "Hello!"},
\t\t},
\t})

\treq, _ := http.NewRequest("POST",
\t\tbaseURL+"/v1/chat/completions",
\t\tbytes.NewReader(body))
\treq.Header.Set("Authorization", "Bearer "+apiKey)
\treq.Header.Set("Content-Type", "application/json")

\tresp, err := http.DefaultClient.Do(req)
\tif err != nil {
\t\tpanic(err)
\t}
\tdefer resp.Body.Close()

\tdata, _ := io.ReadAll(resp.Body)
\tfmt.Println(string(data))
}`}export{v as ApiKeyCodeDialog};