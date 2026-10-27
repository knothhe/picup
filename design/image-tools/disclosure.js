// Standalone artboard interaction: opening options never starts a task.
const fieldSelectors = {format: '#format', quality: '#quality', width: '#width', compress: '.compress-input'};
for (const panel of document.querySelectorAll('.parameters')) {
  const english = document.documentElement.lang === 'en';
  const summary = panel.querySelector('summary');
  const format = panel.querySelector('#format');
  const quality = panel.querySelector('#quality');
  const width = panel.querySelector('#width');
  const compress = panel.querySelector('.compress-input');
  const locked = panel.classList.contains('locked');
  const updateLabel = () => summary.setAttribute('aria-label', panel.open ? (english ? 'Hide processing options' : '收起处理选项') : (english ? 'Show processing options' : '展开处理选项'));
  panel.addEventListener('toggle', updateLabel);
  // A locked editor that is already open may close, but a closed one stays shut.
  summary.addEventListener('click', event => {
    if (locked && !panel.open) event.preventDefault();
  });
  const chipLabels = english
    ? {format: 'Change output format', compress: 'Change compression', quality: 'Change quality', width: 'Change maximum width'}
    : {format: '修改输出格式', compress: '修改压缩设置', quality: '修改压缩质量', width: '修改最大宽度'};
  const bindChip = chip => chip.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    panel.open = true;
    panel.querySelector(fieldSelectors[chip.dataset.field])?.focus();
  });
  panel.querySelectorAll('button.chip').forEach(bindChip);
  const update = () => {
    const labels = {original: english ? 'Original format' : '原格式', webp:'WebP', avif:'AVIF', jpeg:'JPEG', png:'PNG'};
    const parts = [[labels[format.value], 'format']];
    if (format.value === 'original') parts.push([compress.checked ? (english ? 'Compress' : '压缩') : (english ? 'No compression' : '不压缩'), 'compress']);
    else if (format.value === 'png') parts.push([english ? 'Lossless' : '无损', 'format']);
    else parts.push([english ? `Quality ${quality.value}` : `质量 ${quality.value}`, 'quality']);
    if (+width.value > 0) parts.push([english ? `Max ${width.value} px` : `最宽 ${width.value} px`, 'width']);
    const chips = parts.map(([text, field]) => {
      const chip = document.createElement(locked ? 'span' : 'button');
      chip.className = 'chip';
      chip.dataset.field = field;
      chip.textContent = text;
      if (!locked) {
        chip.type = 'button';
        chip.title = chip.ariaLabel = chipLabels[field];
        bindChip(chip);
      }
      return chip;
    });
    panel.querySelector('.processing-summary').replaceChildren(...chips);
    quality.disabled = format.disabled || format.value === 'png';
  };
  for (const control of [format, quality, width, compress]) control.addEventListener('input', update);
}
