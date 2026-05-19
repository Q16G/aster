<?php
// PHP 文件下载 / 任意文件读取漏洞测试用例
// 仅用于 semgrep 规则验证

// =================== 直接型 ===================

// ruleid: php-download-arbitrary-file
readfile($_GET['file']);

// ruleid: php-download-arbitrary-file
$content = file_get_contents($_REQUEST['name']);

// ruleid: php-download-arbitrary-file
$lines = file($_POST['p']);

// ruleid: php-download-arbitrary-file
$fp = fopen($_GET['name'], 'rb');

// ruleid: php-download-arbitrary-file
$obj = new SplFileObject($_GET['f']);

// =================== 路径前缀拼接 ===================

$name = $_GET['name'];
// ruleid: php-download-arbitrary-file
readfile('/var/data/' . $name);

// ruleid: php-download-arbitrary-file
$content = file_get_contents('/var/data/' . $_GET['file']);

// ruleid: php-download-arbitrary-file
$fp = fopen('/var/data/' . $_POST['name'], 'rb');

// =================== 压缩文件 ===================

// ruleid: php-download-arbitrary-file
$z = zip_open($_GET['file']);

// ruleid: php-download-arbitrary-file
$gz = gzopen($_GET['name'], 'rb');

// =================== mime / finfo 探测 ===================

// ruleid: php-download-arbitrary-file
$type = mime_content_type($_GET['file']);

// =================== Laravel ===================

function laravelDownload() {
    // ruleid: php-download-arbitrary-file
    return response()->download(request()->input('file'));
}

function laravelStorage() {
    // ruleid: php-download-arbitrary-file
    return Storage::download(request('file'));
}

function laravelFileGet() {
    // ruleid: php-download-arbitrary-file
    return File::get(request('p'));
}

// =================== Symfony ===================

function symfonyBinary() {
    // ruleid: php-download-arbitrary-file
    return new BinaryFileResponse($_GET['file']);
}

// =================== ThinkPHP ===================

function thinkDownload() {
    // ruleid: php-download-arbitrary-file
    return download($_GET['file']);
}

// =================== 安全写法 ===================

// 字面量
// ok: php-download-arbitrary-file
readfile("/var/data/report.csv");

// basename 净化
// ok: php-download-arbitrary-file
readfile('/var/data/' . basename($_GET['file']));

// ok: php-download-arbitrary-file
$content = file_get_contents(basename($_GET['name']));

// ok: php-download-arbitrary-file
$fp = fopen('/var/data/' . basename($_GET['name']), 'rb');

// pathinfo 净化
// ok: php-download-arbitrary-file
readfile('/var/data/' . pathinfo($_GET['file'], PATHINFO_BASENAME));
