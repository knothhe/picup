/* A local motion specification. This file does not use application services. */
const query = new URLSearchParams(location.search);
const settings = { profiles: '配置', storage: '存储', images: '图片与输出', general: '通用', cli: 'CLI', shortcuts: '快捷键', about: '关于' };
const pages = { upload: '上传', gallery: '图库', settings: '设置', guide: '指南' };
const motion = { duration: 240, easing: 'cubic-bezier(.42,0,.58,1)' };
const animations = new WeakMap();
let mainSelected = 'upload';
let settingsSelected = query.get('settings') in settings ? query.get('settings') : 'general';
let galleryPage = 1;
let galleryPending = 0;
let galleryTimer;
const galleryCache = new Set([1]);
const reduceMotion = () => query.get('reduce') === '1' || matchMedia('(prefers-reduced-motion: reduce)').matches;
const svg = (path) => `<svg viewBox="0 0 24 24" aria-hidden="true">${path}</svg>`;
const cloud = svg('<path d="M7 18H6a4 4 0 1 1 1-8 6 6 0 0 1 11-2 5 5 0 1 1 1 10h-2"/><path d="M12 20V10m-3 3 3-3 3 3"/>');
const group = (title, rows) => `<section class="group"><h2>${title}</h2><div class="rows">${rows}</div></section>`;
const row = (title, description, control, extra = '') => `<div class="row ${extra}"><div class="label">${title}${description ? `<small>${description}</small>` : ''}</div><div class="control">${control}</div></div>`;
const toggle = (on = true) => `<button class="switch ${on ? '' : 'off'}" role="switch" aria-checked="${on}" aria-label="切换偏好"></button>`;
const segmented = (labels, chosen = 0) => `<div class="segmented">${labels.map((label, index) => `<button class="${index === chosen ? 'chosen' : ''}" aria-pressed="${index === chosen}">${label}</button>`).join('')}</div>`;
const select = (labels) => `<select>${labels.map((label) => `<option>${label}</option>`).join('')}</select>`;
const tabs = () => `<nav class="settings-tabs" aria-label="设置分类">${Object.entries(settings).map(([key, label]) => `<button data-settings="${key}" class="${key === settingsSelected ? 'selected' : ''}" aria-current="${key === settingsSelected ? 'page' : 'false'}">${label}</button>`).join('')}</nav>`;

function settingsBody(key) {
  if (key === 'general') return group('界面',
    row('语言', '无匹配的系统语言时使用英文。', select(['跟随系统', '简体中文', 'English'])) +
    row('外观', '即时生效并自动保存。', segmented(['自动', '浅色', '深色']))) +
    group('窗口与启动',
      row('关闭窗口后继续运行', '关闭主窗口后保留托盘和后台上传；关闭此选项后退出应用。', toggle()) +
      row('Dock 图标', '跟随窗口时，仅在主窗口打开时展示。', segmented(['展示', '隐藏', '跟随窗口'], 2), 'stacked') +
      row('点击托盘图标', '选择点击菜单栏或托盘图标时打开的位置。', segmented(['打开快捷面板', '打开主窗口']), 'stacked') +
      row('轻量模式', '窗口和面板全部关闭一分钟后释放界面内存，再次打开时重新加载。', toggle(false))) +
    group('图库缓存', row('本地缓存', '保存列表与缩略图，最多 256 MiB，自动清理较早使用的内容。', '<button>清理图库缓存</button>')) + '<p>清理缓存后，下次打开图库会重新加载。</p>';
  if (key === 'images') return group('图片处理',
    row('默认压缩图片', 'PNG 无损优化，JPEG / WebP / AVIF 按质量压缩。', toggle()) +
    row('图片格式', '动画保持原样；HEIC 默认转为 WebP。', select(['保留原格式', 'WebP', 'AVIF', 'JPEG', 'PNG'])) +
    row('压缩质量', '适用于 JPEG / WebP / AVIF，范围 1 至 100。', '<input class="number" type="number" value="85" min="1" max="100" aria-label="压缩质量">') +
    row('最大宽度', '单位为像素，0 为不限；只缩小，不放大。', '<input class="number" type="number" value="0" min="0" aria-label="最大宽度">')) +
    group('链接与剪贴板', row('链接格式', '上传完成后复制的内容格式。', select(['URL', 'Markdown', 'JSON'])) + row('上传后自动复制结果', 'CLI 在交互终端中使用此偏好。', toggle()));
  if (key === 'storage') return '<div class="editing-profile"><div><small>当前配置</small><strong>默认</strong></div><button class="ghost" data-settings="profiles">管理配置</button></div><section class="storage-fields"><h2>存储连接</h2><label>服务商' + select(['Cloudflare R2', 'S3']) + '</label><label>Endpoint<input value="https://ACCOUNT.r2.cloudflarestorage.com"><small>服务地址，不含 Bucket。</small></label><div class="columns"><label>Bucket<input value="photos"></label><label>Region<input value="auto"></label></div><h2>访问凭据</h2><label>Access key<input type="password" value="local-demo-key"></label><label>Secret key<input type="password" value="local-demo-secret"><small>保存在本机配置文件。</small></label><h2>公开链接</h2><label>公开访问地址<input value="https://img.example.com"></label><label>路径模板<input value="images/{sha256}.{extName}"><small>目录与文件名。</small></label><h2>连接检查</h2><p>测试会创建临时对象并删除。</p><div><button>测试上传与清理</button></div></section>';
  if (key === 'cli') return group('命令行工具', row('安装目录', '默认安装到用户目录。', '<input value="~/.local/bin" aria-label="安装目录">') + row('安装或更新', '与桌面端共用配置。', '<button class="primary">安装 / 更新 CLI</button>')) + '<p>安装后请将此目录加入 PATH。</p><section class="panel"><h2>快速开始</h2><code>picup photo.png<br>picup --output markdown<br>picup config</code></section>';
  if (key === 'shortcuts') return group('键盘快捷键', row('切换侧边栏', '', '<div class="shortcut"><button>⌘ B</button><button class="ghost reset">恢复默认</button></div>') + row('选择图片', '', '<div class="shortcut"><button>⌘ O</button><button class="ghost reset">恢复默认</button></div>') + row('从剪贴板粘贴', '', '<div class="shortcut"><button>⌘ V</button><button class="ghost reset">恢复默认</button></div>')) + '<p>点击快捷键后按下新的组合键。</p>';
  if (key === 'about') return '<div class="about-brand"><img src="../../assets/icon.svg" alt="Picup"><div><h2>Picup</h2><p>轻量桌面图床工具</p></div></div>' + group('本机信息', row('版本', '', '<span>0.1.0</span>') + row('系统', '', '<span>macOS arm64</span>'));
  return '<div class="heading"><div><h2>配置</h2><p>管理用于上传和图库的存储配置。</p></div><button>新建配置</button></div><section class="profile-card"><h3>默认 <span class="badge">当前配置</span></h3><p>R2 · photos</p><div class="profile-actions"><button data-settings="storage">编辑存储</button><button>重命名</button><button>复制</button><button disabled>删除</button></div></section><section class="profile-card"><h3>网站图片</h3><p>S3 · website-assets</p><div class="profile-actions"><button>切换到此配置</button><button>重命名</button><button>复制</button><button>删除</button></div></section>';
}

function galleryImages(page) {
  return Array.from({ length: 6 }, (_, index) => `<img src="../gallery/assets/photo-${(index + (page - 1) * 3) % 6}.svg" alt="第 ${page} 页图片 ${index + 1}">`).join('');
}

function galleryGrid(page) {
  return `<div class="photo-grid" aria-label="第 ${page} 页图片">${galleryImages(page)}</div>`;
}

function galleryContent() {
  return `<div class="heading gallery-heading"><div><h1>图库</h1></div><span class="destination">${cloud}R2 · photos</span><span class="push"></span></div><div class="gallery-toolbar"><button class="icon primary" aria-label="刷新图库" title="刷新图库">${svg('<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 7a7 7 0 0 1 11.5-2L20 8M4 16l2.4 3A7 7 0 0 0 18 17"/>')}</button><button class="icon" aria-label="选择图片" title="选择图片">${svg('<rect x="3" y="3" width="18" height="18" rx="2"/><path d="m7 12 3 3 7-7"/>')}</button><input placeholder="按图片名称搜索..." aria-label="按图片名称搜索"><button class="icon" aria-label="过滤选项" title="过滤选项">${svg('<path d="M4 3h16l-6 7v8l-4 3V10Z"/>')}</button><button class="icon" aria-label="排序选项" title="排序选项">${svg('<path d="m3 8 4-4 4 4M7 4v16m6-4 4 4 4-4m-4 4V4"/>')}</button></div><div class="photo-grid" aria-label="第 ${galleryPage} 页图片">${galleryImages(galleryPage)}</div><div class="pagination"><button class="page-step" data-gallery="previous" ${galleryPage === 1 ? 'disabled' : ''}>‹ 上一页</button>${[1, 2, 3].map((page) => `<button data-gallery="${page}" class="${page === galleryPage ? 'primary' : ''}" aria-current="${page === galleryPage ? 'page' : 'false'}">${page}</button>`).join('')}<button class="page-step" data-gallery="next" ${galleryPage === 3 ? 'disabled' : ''} aria-label="下一页">下一页 ›</button><span class="gallery-state sr-only" role="status">第 ${galleryPage} 页 · 共 3 页</span></div>`;
}

function pageContent(key) {
  if (key === 'gallery') return galleryContent();
  if (key === 'settings') return `<div class="heading"><div><h1>设置</h1><p>配置存储、图片处理和桌面偏好。</p></div><button data-main="guide">使用入门</button></div>${tabs()}<div class="stage settings-stage" aria-label="设置内容"></div><div class="footer"><span>更改会自动保存，桌面端与 CLI 共用设置。</span></div>`;
  if (key === 'guide') return '<div class="heading"><div><h1>使用入门</h1><p>完成配置，上传第一张图片。</p></div><button data-open-storage>打开存储设置</button></div><div class="scroll fill"><section class="panel"><div class="step-head"><span class="num">01</span><h2>连接你的存储</h2></div><p>在设置中选择 S3 或 R2，填写存储桶、访问凭据和图片公开地址。</p><p>R2 的 Region 使用 auto；AWS S3 使用桶所在区域，Endpoint 可留空。</p></section><section class="panel"><div class="step-head"><span class="num">02</span><h2>检查连接</h2></div><p>设置会自动保存。填写完成后点击「测试上传与清理」，测试会创建并删除临时对象，需要写入和删除权限。</p></section><section class="panel"><div class="step-head"><span class="num">03</span><h2>上传与管理</h2></div><p>拖入图片、选择文件或粘贴后上传。菜单栏还可直接选择图片或上传剪贴板。</p><p>成功后复制链接，在图库浏览、搜索和管理云端图片。图库缓存列表与缩略图，启动后先显示缓存再刷新。</p></section><section class="panel"><details><summary>路径变量</summary><p>默认按内容哈希命名。可使用日期和扩展名变量自定义目录。</p><code>{year}/{month}-{day}-{md5}.{extName}</code></details><details><summary>命令行</summary><p>在「设置 → CLI」安装命令行工具，与桌面端共用配置。</p><code>picup photo.png</code></details><details><summary>连接提示</summary><p>Endpoint 不含 Bucket。Path-style 开启时 Bucket 位于请求路径，关闭时位于域名前。</p></details></section></div>';
  return `<div class="heading upload-heading"><div><h1>上传</h1></div><span class="destination">${cloud}R2 · photos</span><span class="push"></span><button class="ghost" data-open-storage>存储设置</button></div><section class="drop-zone"><img src="../../assets/icon.svg" alt=""><div class="drop-title">拖入图片，或 <button>选择图片</button></div><p>PNG · JPEG · WebP · GIF · HEIC · AVIF</p><button class="paste">${svg('<path d="M9 4H6a2 2 0 0 0-2 2v14h16V6a2 2 0 0 0-2-2h-3"/><rect x="9" y="2" width="6" height="4" rx="1"/>')}从剪贴板粘贴 <kbd>⌘ V</kbd></button></section><div class="fill"></div><div class="footer upload-footer">${svg('<path d="m12 3 3 6 6 3-6 3-3 6-3-6-6-3 6-3z"/>')}<span>默认压缩 · 质量 85</span><button data-open-images>图片设置</button><span class="push foot-note">添加后手动开始上传</span></div>`;
}

function layer(key, content, nested = false) {
  const element = document.createElement('section');
  element.className = 'layer';
  element.dataset.key = key;
  element.innerHTML = content;
  if (!nested && key === 'settings') element.querySelector('.settings-stage').append(layer(settingsSelected, settingsBody(settingsSelected), true));
  if (!nested && key === 'gallery') {
    const grid = element.querySelector('.photo-grid');
    const stage = document.createElement('div');
    stage.className = 'stage gallery-stage';
    grid.replaceWith(stage);
    const page = layer(String(galleryPage), '', true);
    page.append(grid);
    stage.append(page);
  }
  return element;
}

function animate(element, frames, removeAfter = false) {
  const animation = element.animate(frames, { ...motion, fill: 'forwards' });
  animations.set(element, animation);
  animation.onfinish = () => {
    if (removeAfter) element.remove();
    else if (element.isConnected) { animation.commitStyles(); animation.cancel(); }
  };
}

function replace(container, next, immediate = false) {
  if (immediate || reduceMotion()) {
    [...container.children].forEach((child) => animations.get(child)?.cancel());
    container.replaceChildren(next);
    return;
  }
  [...container.children].forEach((child) => {
    const computed = getComputedStyle(child);
    const opacity = computed.opacity;
    const transform = computed.transform;
    animations.get(child)?.cancel();
    child.classList.add('retiring');
    child.inert = true;
    child.setAttribute('aria-hidden', 'true');
    animate(child, [{ opacity, transform }, { opacity: 0, transform }], true);
  });
  container.append(next);
  animate(next, [{ opacity: 0, transform: 'translateY(6px)' }, { opacity: 1, transform: 'translateY(0)' }]);
}

function updateNavigation() {
  document.body.dataset.page = mainSelected;
  document.querySelector('.breadcrumb').textContent = pages[mainSelected];
  document.querySelectorAll('[data-main]').forEach((button) => {
    const selected = button.dataset.main === mainSelected;
    button.classList.toggle('selected', selected);
    button.setAttribute('aria-current', selected ? 'page' : 'false');
  });
  const nav = document.querySelector('.sidebar-settings');
  nav.innerHTML = '<small>设置</small>' + Object.entries(settings).map(([key, label]) => `<button class="ghost ${key === settingsSelected ? 'selected' : ''}" data-settings="${key}" aria-current="${key === settingsSelected ? 'page' : 'false'}">${label}</button>`).join('');
  document.querySelectorAll('.settings-tabs [data-settings]').forEach((button) => {
    const selected = button.dataset.settings === settingsSelected;
    button.classList.toggle('selected', selected);
    button.setAttribute('aria-current', selected ? 'page' : 'false');
  });
}

function navigate(key, immediate = false) {
  if (!(key in pages) || (key === mainSelected && document.querySelector('.main-stage').children.length)) return;
  mainSelected = key;
  replace(document.querySelector('.main-stage'), layer(key, pageContent(key)), immediate);
  updateNavigation();
  if (galleryPending) updateGallery();
}

function changeSettings(key, immediate = false) {
  if (!(key in settings) || key === settingsSelected) return;
  settingsSelected = key;
  if (mainSelected !== 'settings') { navigate('settings', immediate); return; }
  const stage = document.querySelector('.main-stage>.layer:not(.retiring) .settings-stage');
  if (stage) replace(stage, layer(key, settingsBody(key), true), immediate);
  updateNavigation();
}

function updateGallery() {
  document.querySelectorAll('.main-stage>.layer:not(.retiring)[data-key=gallery]').forEach((page) => {
    const stage = page.querySelector('.gallery-stage');
    let current = stage.querySelector('.layer:not(.retiring)');
    if (!galleryPending && current.dataset.key !== String(galleryPage)) {
      current = layer(String(galleryPage), galleryGrid(galleryPage), true);
      replace(stage, current);
    }
    const grid = current.querySelector('.photo-grid');
    const loading = Boolean(galleryPending);
    grid.inert = loading;
    grid.setAttribute('aria-busy', String(loading));
    grid.setAttribute('aria-disabled', String(loading));
    page.querySelectorAll('[data-gallery]').forEach((button) => {
      const key = button.dataset.gallery;
      const current = Number(key) === galleryPage;
      button.classList.toggle('primary', current);
      button.setAttribute('aria-current', current ? 'page' : 'false');
      if (key === 'next') {
        button.innerHTML = loading ? svg('<path d="m6 6 12 12M6 18 18 6"/>') : '下一页 ›';
        button.setAttribute('aria-label', loading ? '取消请求' : '下一页');
        button.title = loading ? '取消请求' : '下一页';
        button.disabled = !loading && galleryPage === 3;
      } else {
        button.innerHTML = current && loading ? '<span class="spinner" aria-hidden="true"></span>' : key === 'previous' ? '‹ 上一页' : key;
        button.disabled = loading || (key === 'previous' && galleryPage === 1);
        if (key !== 'previous') button.setAttribute('aria-label', current && loading ? `当前第 ${galleryPage} 页，正在加载第 ${galleryPending} 页` : `第 ${key} 页`);
      }
    });
    page.querySelector('.gallery-state').textContent = loading ? `正在加载第 ${galleryPending} 页，可取消请求。当前仍为第 ${galleryPage} 页。` : `第 ${galleryPage} 页 · 共 3 页`;
  });
}

function changeGallery(target) {
  if (galleryPending) {
    if (target === 'next') {
      clearTimeout(galleryTimer);
      galleryPending = 0;
      updateGallery();
    }
    return;
  }
  const next = target === 'previous' ? galleryPage - 1 : target === 'next' ? galleryPage + 1 : Number(target);
  if (next < 1 || next > 3 || next === galleryPage) return;
  clearTimeout(galleryTimer);
  if (galleryCache.has(next)) { galleryPage = next; galleryPending = 0; updateGallery(); return; }
  galleryPending = next;
  updateGallery();
  galleryTimer = setTimeout(() => {
    galleryCache.add(next);
    galleryPage = next;
    galleryPending = 0;
    updateGallery();
  }, 850);
}

function frozenState(scope, from, to, phase) {
  const content = (key) => scope === 'gallery' ? galleryGrid(key) : scope === 'settings' ? settingsBody(key) : pageContent(key);
  const old = layer(String(from), content(from), scope !== 'main');
  const next = layer(String(to), content(to), scope !== 'main');
  const stage = document.querySelector(scope === 'gallery' ? '.gallery-stage' : scope === 'settings' ? '.settings-stage' : '.main-stage');
  const progress = phase === 'start' ? 0 : phase === 'middle' ? .5 : 1;
  old.inert = true;
  old.classList.add('retiring');
  old.setAttribute('aria-hidden', 'true');
  old.style.opacity = String(1 - progress);
  next.style.opacity = String(progress);
  next.style.transform = `translateY(${6 * (1 - progress)}px)`;
  stage.replaceChildren(...(progress === 1 ? [next] : [old, next]));
}

document.body.classList.toggle('collapsed', query.get('collapsed') === '1');
document.body.classList.toggle('dark', query.get('dark') === '1');
const scope = query.get('scope') || 'main';
const phase = query.get('phase');
if (scope === 'gallery') {
  const from = Number(query.get('from')) || 1;
  const to = Number(query.get('to')) || 2;
  galleryPage = phase ? to : from;
  navigate('gallery', true);
  if (phase) frozenState('gallery', from, to, reduceMotion() ? 'end' : phase);
} else if (scope === 'settings') {
  const from = query.get('from') in settings ? query.get('from') : 'general';
  const to = query.get('to') in settings ? query.get('to') : 'images';
  settingsSelected = phase ? to : from;
  navigate('settings', true);
  if (phase) frozenState('settings', from, to, reduceMotion() ? 'end' : phase);
} else {
  const from = query.get('from') in pages ? query.get('from') : 'upload';
  const to = query.get('to') in pages ? query.get('to') : 'gallery';
  navigate(phase ? to : from, true);
  if (phase) frozenState('main', from, to, reduceMotion() ? 'end' : phase);
}

if (mainSelected === 'gallery' && query.get('gallery') === 'loading') {
  galleryPending = 2;
  updateGallery();
}

document.addEventListener('click', (event) => {
  const button = event.target.closest('button');
  if (!button || button.disabled || button.closest('[inert]')) return;
  if (button.hasAttribute('data-main')) navigate(button.dataset.main);
  if (button.hasAttribute('data-settings')) changeSettings(button.dataset.settings);
  if (button.hasAttribute('data-gallery')) changeGallery(button.dataset.gallery);
  if (button.hasAttribute('data-open-storage')) { settingsSelected = 'storage'; navigate('settings'); }
  if (button.hasAttribute('data-open-images')) { settingsSelected = 'images'; navigate('settings'); }
  if (button.hasAttribute('data-toggle-sidebar')) document.body.classList.toggle('collapsed');
  if (button.classList.contains('switch')) {
    const checked = button.getAttribute('aria-checked') !== 'true';
    button.setAttribute('aria-checked', String(checked));
    button.classList.toggle('off', !checked);
  }
  if (button.closest('.segmented')) {
    button.parentElement.querySelectorAll('button').forEach((choice) => {
      choice.classList.toggle('chosen', choice === button);
      choice.setAttribute('aria-pressed', String(choice === button));
    });
  }
});
