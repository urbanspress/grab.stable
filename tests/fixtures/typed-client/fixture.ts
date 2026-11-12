import assert from 'node:assert/strict';
import React, {StrictMode} from 'react';
import {act,create} from 'react-test-renderer';
import {GrabClient} from './generated/client.js';
import {grabInteger,grabDecimal,integerToBigInt} from './generated/types.js';
import {operation,parseWire,validateInput,validateOutput} from './generated/validators.js';
import {use_product,mutate_createProduct} from './generated/react.js';
Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});
const product={id:grabInteger('90071992547409931234567890'),price:grabDecimal('19.90'),name:'Book',bytes:'AAE=',note:null};
assert.equal(integerToBigInt(product.id),90071992547409931234567890n);
assert.throws(()=>grabInteger('01'));assert.throws(()=>grabDecimal('1e2'));
validateOutput(operation('product'),200,product);
for(const x of [{...product,id:123},{...product,price:'19.9'},{...product,unexpected:1},{...product,bytes:'AB=='}])assert.throws(()=>validateOutput(operation('product'),200,x));
assert.throws(()=>parseWire('{"x":1,"x":2}'));assert.throws(()=>parseWire('{"x":{"a":1,"a":2}}'));
assert.throws(()=>validateInput(operation('product'),{path:{id:'1'},query:{wrong:'x'},body:null}));
let reads=0,mutations=0,aborts=0;let urls:string[]=[];
const mock:typeof fetch=async(url,options)=>{
 urls.push(String(url));if(options?.method==='POST'){mutations++;return new Response(JSON.stringify(product),{status:201});}
 reads++;return new Promise<Response>((resolve,reject)=>{const timer=setTimeout(()=>resolve(new Response(JSON.stringify(product))),5);options?.signal?.addEventListener('abort',()=>{aborts++;clearTimeout(timer);reject(new DOMException('Aborted','AbortError'));},{once:true});});
};
const client=new GrabClient('',10000,mock);
const input={path:{id:grabInteger('1')},query:{search:'a/b & c'},body:null};
const res=await client.call('product',input);assert.equal(res.value.price,'19.90');assert(urls[0].includes('search=a%2Fb+%26+c'));
let state:ReturnType<typeof use_product>|undefined;
function View(){state=use_product(client,input);return React.createElement('div',null,state.loading?'loading':state.data?.value.name);}
let root:ReturnType<typeof create>;
await act(async()=>{root=create(React.createElement(StrictMode,null,React.createElement(View)));});
await act(async()=>{await new Promise(r=>setTimeout(r,30));});
assert.equal(state!.data!.value.id,product.id);assert.equal(mutations,0);assert(aborts>=1);assert(reads>=3);
await act(async()=>{state!.refetch();});await act(async()=>{await new Promise(r=>setTimeout(r,30));});assert.equal(state!.loading,false);
await act(async()=>{root.unmount();});
await mutate_createProduct(client,{path:{},query:{},body:product});assert.equal(mutations,1);
const invalid=new GrabClient('',10000,async()=>new Response('{"id":"1","id":"2"}'));
await assert.rejects(invalid.call('product',input));
const canceled=new AbortController();canceled.abort();let sawAbort=false;
const cancelClient=new GrabClient('',10000,async(_,options)=>{sawAbort=!!options?.signal?.aborted;throw new Error('canceled');});await assert.rejects(cancelClient.call('product',input,{signal:canceled.signal}));assert(sawAbort);
console.log('Compiled TypeScript/React fixture passed: exact values, validation, cancellation, Strict Mode, explicit mutations.');
