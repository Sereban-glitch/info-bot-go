// Focused source recovered against the shipped application, not original React source.
// Keep receipt and outcome separate. A classification flag alone is not a receipt.
function _o(request) {
  if (request.status === 'bounced' || request.lastStatus === 'error_message') return 'bounced';
  if (request.lastStatus === 'rejected' || request.status === 'refused') return 'refused';
  if (request.lastStatus === 'partially_successful' || request.status === 'partial') return 'partial';
  if (request.lastStatus === 'successful') return 'replied';
  if (request.repliedAt || request.status === 'replied' || request.status === 'unrated') return 'unrated';
  if (request.status !== 'pending') return request.status;
  const deadline = fo(new Date(request.createdAt), 5);
  return new Date() > deadline ? 'overdue' : 'pending';
}
