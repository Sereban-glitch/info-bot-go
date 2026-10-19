// Same portal successful/all metric and minimum-five scope stated by the rating.
function szSuccessRate(item) {
  const total = Number(item.requests), successful = Number(item.successful ?? 0);
  if (!Number.isInteger(total) || !Number.isInteger(successful) || total < 5 || successful < 0 || successful > total) return null;
  return `${(successful / total * 100).toLocaleString('uk-UA', {maximumFractionDigits: 1})}%`;
}
