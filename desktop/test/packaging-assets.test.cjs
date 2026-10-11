const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const desktopRoot = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(desktopRoot, relativePath), 'utf8');
}

// 去掉行注释后再解析，避免注释里的引号或关键词干扰匹配。
function stripLineComments(source) {
  return source.replace(/^\s*\/\/.*$/gm, '');
}

function builderFiles(cleanedSource) {
  const block = cleanedSource.match(/files:\s*\[([\s\S]*?)\]/);
  assert.ok(block, 'electron-builder.mjs 必须定义 files 白名单');
  return [...block[1].matchAll(/'([^']+)'/g)].map(match => match[1]);
}

// main.cjs 在运行时用 path.join(__dirname, 'assets', '<file>') 读取窗口 / Dock 图标。
// buildResources 目录默认不随 app 打包，所以每个运行时引用都必须在 files 白名单里显式列出，
// 否则图标会静默丢失（fs.existsSync 为假，不报错、不崩溃，只是没图标）。
test('every runtime asset referenced by main.cjs is packaged into the app', () => {
  const mainSource = read('main.cjs');
  const referenced = new Set(
    [...mainSource.matchAll(/path\.join\(__dirname,\s*'assets',\s*'([^']+)'\)/g)].map(match => match[1]),
  );
  assert.ok(referenced.size > 0, 'main.cjs 应当至少引用一个运行时资源');

  const files = builderFiles(stripLineComments(read(path.join('scripts', 'electron-builder.mjs'))));
  for (const name of referenced) {
    const target = `assets/${name}`;
    assert.ok(
      files.includes(target),
      `${target} 被 main.cjs 在运行时读取，但不在 electron-builder 的 files 白名单中：${files.join(', ')}`,
    );
  }
});

// 无签名证书时，必须只关「文件签名」而保留 resedit 资源编辑（exe 图标 + 版本元数据）。
// 若改成连资源编辑一起关掉的粗粒度开关，打包出的 exe 会退化成默认 Electron 图标且无版本信息。
test('packaging keeps exe icon and version metadata while skipping code signing', () => {
  const source = stripLineComments(read(path.join('scripts', 'electron-builder.mjs')));
  assert.match(source, /signExecutable:\s*false/, '无证书构建应使用 signExecutable: false');
  assert.doesNotMatch(source, /signAndEditExecutable:\s*false/, '不要用 signAndEditExecutable: false，它会连带跳过图标与版本元数据');
});
