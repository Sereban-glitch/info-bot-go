// Restore the shipped countdown without changing the existing status/date rules.
function szDeadlineInfo(request) {
  const status = _o(request);
  if (!['pending', 'overdue'].includes(status)) return null;
  const days = lo(request.deadline);
  if (days === null) return null;
  return {
    label: days < 0 ? `+${Math.abs(days)} ${U(days)}` : days === 0 ? 'Сьогодні' : `${days} ${U(days)}`,
    tone: status === 'overdue' || days < 0 ? 'bad' : days <= 1 ? 'warn' : 'muted',
    date: co(request.deadline)
  };
}
// Both the overview and filtered list open the same authenticated request detail.
function xs({req}) {
  const status = _o(req);
  const deadline = szDeadlineInfo(req);
  return sz(Vr, {to: '/requests/$requestId', params: {requestId: req.id},
    className: 'sz-request-card', 'data-status': status},
    sz('span', {className: 'sz-request-icon'}, sz(SzIcon, {name: 'document'})),
    sz('div', {className: 'sz-request-copy'},
      sz('p', {className: 'sz-request-subject'}, req.subject),
      sz('p', {className: 'sz-request-recipient'}, req.recipientName),
      sz('div', {className: 'sz-request-meta'}, sz(_s, {status}),
        sz('span', {}, req.channel === 'dostup' ? 'Портал' : 'Email'),
        sz('span', {className: 'sz-request-date'}, `Надіслано: ${co(req.createdAt) || 'дату не вказано'}`))),
    sz('div', {className: 'sz-request-edge'}, sz(SzIcon, {name: 'chevron'}),
      deadline ? sz('div', {className: 'sz-request-countdown', 'data-tone': deadline.tone,
        'aria-label': `Розрахунковий строк: ${deadline.date}. ${deadline.label}`},
        sz('small', {}, 'Орієнтир'), sz('strong', {}, deadline.label))
        : req.repliedAt && ['replied', 'unrated', 'refused', 'partial'].includes(status)
          ? sz('span', {className: 'sz-request-reply-date'}, `Відповідь: ${co(req.repliedAt)}`) : null));
}
