// Mutually exclusive display categories; every received refusal is still an answer.
function Ss(requests) {
  const result = {total: requests.length, replied: 0, pending: 0, overdue: 0, refused: 0, partial: 0, bounced: 0, answered: 0};
  for (const request of requests) {
    const status = _o(request);
    if (status === 'unrated') result.replied++;
    else if (Object.hasOwn(result, status)) result[status]++;
    if (['replied', 'unrated', 'refused', 'partial'].includes(status)) result.answered++;
  }
  return result;
}
