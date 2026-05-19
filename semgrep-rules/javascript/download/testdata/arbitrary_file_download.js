// JavaScript 文件下载 / 任意文件读取漏洞测试用例
// 仅用于 semgrep 规则验证
const fs = require('fs');
const fse = require('fs-extra');
const path = require('path');
const { exec } = require('child_process');
const AdmZip = require('adm-zip');
const tar = require('tar');
const express = require('express');
const Koa = require('koa');

const app = express();

// ============= Express =============

app.get('/dl1', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    res.sendFile(req.query.file);
});

app.get('/dl2', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    res.download(req.params.name);
});

app.get('/dl3', (req, res) => {
    const name = req.query.name;
    // ruleid: javascript-download-arbitrary-file
    fs.readFile(path.join('/var/data', name), (err, data) => {
        res.send(data);
    });
});

app.get('/dl4', (req, res) => {
    const name = req.headers['x-file'];
    // ruleid: javascript-download-arbitrary-file
    res.send(fs.readFileSync(name));
});

app.get('/dl5', (req, res) => {
    const name = req.body.path;
    // ruleid: javascript-download-arbitrary-file
    fs.createReadStream(name).pipe(res);
});

app.get('/dl6', (req, res) => {
    const name = req.cookies.token;
    // ruleid: javascript-download-arbitrary-file
    fse.readFile('/var/data/' + name).then(d => res.send(d));
});

app.get('/dl7', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    res.attachment(req.query.file);
});

// ============= Koa =============

const koa = new Koa();
koa.use(async ctx => {
    const name = ctx.query.file;
    // ruleid: javascript-download-arbitrary-file
    ctx.attachment(name);
});

// ============= 压缩 =============

app.get('/zip', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    const z = new AdmZip(req.query.file);
});

app.get('/tar', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    tar.x({ file: req.query.f });
});

// ============= require 动态加载 =============

app.get('/load', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    const mod = require(req.query.module);
});

// ============= 子进程 cat =============

app.get('/cat', (req, res) => {
    // ruleid: javascript-download-arbitrary-file
    exec('cat ' + req.query.f, (err, stdout) => res.send(stdout));
});

// ============= 命令行参数 / 环境变量 =============

function cliArg() {
    // ruleid: javascript-download-arbitrary-file
    return fs.readFileSync(process.argv[2]);
}

function envVar() {
    // ruleid: javascript-download-arbitrary-file
    return fs.readFileSync(process.env.FILE_PATH);
}

// ============= URL 搜索参数 =============

app.use((req, res) => {
    const u = new URL(req.url, 'http://localhost');
    const f = u.searchParams.get('file');
    // ruleid: javascript-download-arbitrary-file
    fs.readFile(f, (e, d) => res.send(d));
});

// ============= 安全写法 =============

app.get('/safe1', (req, res) => {
    const name = path.basename(req.query.file);
    // ok: javascript-download-arbitrary-file
    res.sendFile(path.join('/var/data', name));
});

app.get('/safe2', (req, res) => {
    const id = parseInt(req.query.id, 10);
    // ok: javascript-download-arbitrary-file
    fs.readFile('/var/data/' + id + '.bin', (e, d) => res.send(d));
});

app.get('/safe3', (req, res) => {
    // ok: javascript-download-arbitrary-file
    res.sendFile('/var/data/report.csv');
});

app.get('/safe4', (req, res) => {
    const safe = sanitizePath(req.query.file);
    // ok: javascript-download-arbitrary-file
    res.sendFile(path.join('/var/data', safe));
});

app.get('/safe5', (req, res) => {
    // ok: javascript-download-arbitrary-file
    // root 选项会限制 sendFile 的范围，但这里输入仍可控；只测 sanitizer 调用
    const name = sanitizePath(req.query.file);
    res.sendFile(name, { root: '/var/data' });
});

function sanitizePath(p) {
    if (p.includes('..') || p.startsWith('/')) throw new Error('bad');
    return p;
}
