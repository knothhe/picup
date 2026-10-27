// Standalone artboard interaction: opening options never starts a task.
const fieldSelectors = {format: '#format', quality: '#quality', width: '#width', compress: '.compress-input'};
const presets = [
  {id: 'original', zh: ['原图', '不压缩、不转换、不缩放'], en: ['Original', 'No compression, conversion or resizing'], values: {compress: false, format: 'original', quality: 85, width: 0}},
  {id: 'standard', zh: ['标准', '保留格式压缩，质量 85'], en: ['Standard', 'Compress in the same format, quality 85'], values: {compress: true, format: 'original', quality: 85, width: 0}},
  {id: 'balanced', zh: ['均衡', '转为 WebP，质量 85'], en: ['Balanced', 'Convert to WebP, quality 85'], values: {compress: true, format: 'webp', quality: 85, width: 0}},
  {id: 'smallest', zh: ['体积优先', '转为 AVIF，质量 60，最宽 1920 px'], en: ['Smallest', 'Convert to AVIF, quality 60, up to 1920 px'], values: {compress: true, format: 'avif', quality: 60, width: 1920}},
];
const qualityApplies = v => v.format !== 'png' && !(v.format === 'original' && !v.compress);
const matchPreset = v => presets.find(({values: w}) => v.compress === w.compress && v.format === w.format && v.width === w.width && (!qualityApplies(w) || v.quality === w.quality));
// Uploads confirm saves with the app's settings toast instead of the header.
const headerNotice = !location.pathname.includes('upload');

for (const panel of document.querySelectorAll('.parameters')) {
  const english = document.documentElement.lang === 'en';
  const lang = english ? 'en' : 'zh';
  const summary = panel.querySelector('summary');
  const format = panel.querySelector('#format');
  const quality = panel.querySelector('#quality');
  const width = panel.querySelector('#width');
  const compress = panel.querySelector('.compress-input');
  const savedState = panel.querySelector('.saved-state');
  const locked = panel.classList.contains('locked');
  let savedTimer;
  const updateLabel = () => summary.setAttribute('aria-label', panel.open ? (english ? 'Hide processing options' : '收起处理选项') : (english ? 'Show processing options' : '展开处理选项'));
  panel.addEventListener('toggle', updateLabel);
  // A locked editor that is already open may close, but a closed one stays shut.
  summary.addEventListener('click', event => {
    if (locked && !panel.open) event.preventDefault();
  });
  const chipLabels = english
    ? {format: 'Change output format', compress: 'Change compression', quality: 'Change quality', width: 'Change maximum width'}
    : {format: '修改输出格式', compress: '修改压缩设置', quality: '修改压缩质量', width: '修改最大宽度'};
  const values = () => ({compress: compress.checked, format: format.value, quality: +quality.value, width: +width.value});
  const showSaved = () => {
    if (!headerNotice || !savedState) return;
    savedState.hidden = false;
    clearTimeout(savedTimer);
    savedTimer = setTimeout(() => { savedState.hidden = true; }, 2000);
  };
  const closeMenu = () => document.querySelector('.preset-menu')?.remove();
  const openMenu = trigger => {
    closeMenu();
    const current = matchPreset(values());
    const menu = document.createElement('div');
    menu.className = 'preset-menu';
    menu.setAttribute('role', 'menu');
    for (const preset of presets) {
      const item = document.createElement('button');
      item.type = 'button';
      item.setAttribute('role', 'menuitemradio');
      item.setAttribute('aria-checked', String(preset === current));
      const [name, description] = preset[lang];
      item.innerHTML = `<strong>${name}</strong><small>${description}</small>`;
      item.addEventListener('click', () => {
        const v = preset.values;
        compress.checked = v.compress;
        format.value = v.format;
        width.value = v.width;
        if (qualityApplies(v)) quality.value = v.quality;
        closeMenu();
        update();
        showSaved();
      });
      menu.append(item);
    }
    const r = trigger.getBoundingClientRect();
    menu.style.left = `${r.left}px`;
    menu.style.top = `${r.bottom + 4}px`;
    document.body.append(menu);
    menu.querySelector('[aria-checked="true"]')?.focus() ?? menu.querySelector('button').focus();
  };
  const bindPreset = trigger => trigger.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    document.querySelector('.preset-menu') ? closeMenu() : openMenu(trigger);
  });
  const bindChip = chip => chip.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    panel.open = true;
    panel.querySelector(fieldSelectors[chip.dataset.field])?.focus();
  });
  panel.querySelectorAll('button.chip').forEach(bindChip);
  panel.querySelectorAll('button.preset').forEach(bindPreset);
  document.addEventListener('click', event => {
    if (!event.target.closest('.preset-menu')) closeMenu();
  });
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape') closeMenu();
  });
  const update = () => {
    const v = values();
    const labels = {original: english ? 'Original format' : '原格式', webp:'WebP', avif:'AVIF', jpeg:'JPEG', png:'PNG'};
    const parts = [];
    if (!v.compress && v.format === 'original' && v.width === 0) parts.push([english ? 'Kept as is, no processing' : '保持原图，不做处理', 'compress', true]);
    else {
      parts.push([labels[v.format], 'format']);
      if (v.format === 'original') parts.push([v.compress ? (english ? 'Compress' : '压缩') : (english ? 'No compression' : '不压缩'), 'compress']);
      else if (v.format === 'png') parts.push([english ? 'Lossless' : '无损', 'format']);
      else parts.push([english ? `Quality ${v.quality}` : `质量 ${v.quality}`, 'quality']);
      if (v.width > 0) parts.push([english ? `Max ${v.width} px` : `最宽 ${v.width} px`, 'width']);
    }
    const preset = matchPreset(v);
    const presetName = preset ? preset[lang][0] : (english ? 'Custom' : '自定义');
    const trigger = document.createElement(locked ? 'span' : 'button');
    trigger.className = 'preset';
    if (locked) trigger.textContent = presetName;
    else {
      trigger.type = 'button';
      trigger.title = trigger.ariaLabel = english ? 'Processing preset' : '处理预设';
      trigger.setAttribute('aria-haspopup', 'menu');
      trigger.innerHTML = `${presetName}<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 10 4 4 4-4"/></svg>`;
      bindPreset(trigger);
    }
    const chips = parts.map(([text, field, plain]) => {
      const chip = document.createElement(locked ? 'span' : 'button');
      chip.className = plain ? 'chip plain' : 'chip';
      chip.dataset.field = field;
      chip.textContent = text;
      if (!locked) {
        chip.type = 'button';
        chip.title = chip.ariaLabel = chipLabels[field];
        bindChip(chip);
      }
      return chip;
    });
    panel.querySelector('.processing-summary').replaceChildren(trigger, ...chips);
    quality.disabled = format.disabled || format.value === 'png';
  };
  for (const control of [format, quality, width, compress]) control.addEventListener('input', update);
  // Typing stands in for the debounced save of the real app.
  let editTimer;
  for (const control of [format, quality, width, compress]) control.addEventListener('input', () => {
    clearTimeout(editTimer);
    editTimer = setTimeout(showSaved, 350);
  });
}
