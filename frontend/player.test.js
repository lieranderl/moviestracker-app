import {beforeEach,afterEach,expect,test} from "bun:test";

const savedWindow=globalThis.window;
const savedDocument=globalThis.document;
const savedStorage=globalThis.localStorage;
const playerWindow={location:{origin:"https://movies.example"}};
globalThis.window=playerWindow;
await import("./player.js");
let video;

beforeEach(()=>{
 globalThis.window=playerWindow;
 video=new EventTarget();
 video.currentTime=0;
 video.currentSrc="";
 video.textTracks=[];
 video.querySelectorAll=()=>[];
 video.play=()=>Promise.resolve();
 video.pause=()=>{};
 video.load=()=>{};
 video.removeAttribute=()=>{video.currentSrc=""};
 Object.defineProperty(video,"src",{set(value){video.currentSrc=value;video.currentTime=0},get(){return video.currentSrc}});
 globalThis.document={getElementById:()=>video,documentElement:{lang:"en"}};
 globalThis.localStorage={getItem:()=>null,setItem:()=>{}};
});
afterEach(()=>{
 playerWindow.stopHlsVideo();
 globalThis.window=savedWindow;
 globalThis.document=savedDocument;
 globalThis.localStorage=savedStorage;
});

test("switching a file starts at its beginning while changing its stream preserves position",()=>{
 playerWindow.playHlsVideo("https://nas.example/stream?link=one&index=1&play");
 video.currentTime=47;
 playerWindow.playHlsVideo("https://nas.example/gst/one/master.m3u8?index=1&audio=1");
 video.dispatchEvent(new Event("loadedmetadata"));
 expect(video.currentTime).toBe(47);
 playerWindow.playHlsVideo("https://nas.example/stream?link=one&index=2&play",false);
 video.dispatchEvent(new Event("loadedmetadata"));
 expect(video.currentTime).toBe(0);
});

test("retrying the same stream reloads playback and preserves its position",()=>{
 let plays=0;video.play=()=>{plays++;return Promise.resolve()};
 const url="https://nas.example/stream?link=one&index=1&play";
 playerWindow.playHlsVideo(url);
 video.currentTime=47;
 playerWindow.playHlsVideo(url,true,true);
 video.dispatchEvent(new Event("loadedmetadata"));
 expect(plays).toBe(2);
 expect(video.currentTime).toBe(47);
});
