// Both the overview and filtered list open the same authenticated request detail.
function xs({req}) {
  const status = _o(req);
  return sz(Vr, {to: '/requests/$requestId', params: {requestId: req.id},
    className: 'sz-request-card', 'data-status': status},
    sz('span', {className: 'sz-request-icon'}, sz(SzIcon, {name: 'document'})),
    sz('div', {className: 'sz-request-copy'},
      sz('p', {className: 'sz-request-subject'}, req.subject),
      sz('p', {className: 'sz-request-recipient'}, req.recipientName),
      sz('div', {className: 'sz-request-meta'}, sz(_s, {status}),
        sz('span', {}, req.channel === 'dostup' ? 'Портал' : 'Email'))),
    sz('div', {className: 'sz-request-edge'}, sz(SzIcon, {name: 'chevron'}),
      sz('span', {}, co(req.repliedAt && ['replied', 'unrated', 'refused', 'partial'].includes(status) ? req.repliedAt : req.createdAt))));
}
