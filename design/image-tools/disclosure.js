// Standalone artboard interaction: opening options never starts a task.
for (const panel of document.querySelectorAll('.parameters')) {
  const english = document.documentElement.lang === 'en';
  const format = panel.querySelector('#format');
  const quality = panel.querySelector('#quality');
  const width = panel.querySelector('#width');
  const compress = panel.querySelector('.compress-input');
  const updateLabel = () => panel.querySelector('summary').setAttribute('aria-label', panel.open ? (english ? 'Hide processing options' : '收起处理选项') : (english ? 'Show processing options' : '展开处理选项'));
  panel.addEventListener('toggle', updateLabel);
  const update = () => {
    const labels = {original: english ? 'Original format' : '原格式', webp:'WebP', avif:'AVIF', jpeg:'JPEG', png:'PNG'};
    const parts = [labels[format.value]];
    if (format.value === 'original') parts.push(compress.checked ? (english ? 'Compress' : '压缩') : (english ? 'No compression' : '不压缩'));
    else if (format.value === 'png') parts.push(english ? 'Lossless' : '无损');
    else parts.push(english ? `Quality ${quality.value}` : `质量 ${quality.value}`);
    if (+width.value > 0) parts.push(english ? `Max ${width.value} px` : `最宽 ${width.value} px`);
    panel.querySelector('.processing-summary').textContent = parts.join(' · ');
    quality.disabled = format.disabled || format.value === 'png';
  };
  for (const control of [format, quality, width, compress]) control.addEventListener('input', update);
}
