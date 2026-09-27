function post_(path, body) {
  var response = UrlFetchApp.fetch(PUBLIC_URL + '/api/v1/addon/' + path, {
    method: 'post',
    contentType: 'application/json',
    payload: JSON.stringify(body),
    muteHttpExceptions: true,
  });
  if (response.getResponseCode() !== 200) {
    throw new Error(
      'Mail Tracker request failed (' +
        response.getResponseCode() +
        '). Check pairing and instance availability.',
    );
  }
  return JSON.parse(response.getContentText());
}
function formValue_(e, name) {
  var inputs = e.commonEventObject && e.commonEventObject.formInputs;
  var item = inputs && inputs[name];
  return (item && item.stringInputs && item.stringInputs.value && item.stringInputs.value[0]) || '';
}
function homeCard() {
  var section = CardService.newCardSection()
    .addWidget(
      CardService.newTextParagraph().setText(
        'Pair this Gmail account with your self-hosted Mail Tracker instance. Generate a one-use code in Dashboard → Settings.',
      ),
    )
    .addWidget(CardService.newTextInput().setFieldName('pair_code').setTitle('Pairing code'))
    .addWidget(
      CardService.newTextButton()
        .setText('Pair account')
        .setOnClickAction(CardService.newAction().setFunctionName('pairAccount')),
    );
  return CardService.newCardBuilder()
    .setHeader(CardService.newCardHeader().setTitle('Mail Tracker'))
    .addSection(section)
    .build();
}
function pairAccount(e) {
  var code = formValue_(e, 'pair_code');
  if (!code) throw new Error('Enter the pairing code from Settings.');
  post_('pair', { code: code, identityToken: ScriptApp.getIdentityToken() });
  return CardService.newActionResponseBuilder()
    .setNotification(CardService.newNotification().setText('This Gmail account is paired.'))
    .build();
}
function composeCard(e) {
  var recipients = [].concat(
    (e.gmail && e.gmail.toRecipients) || [],
    (e.gmail && e.gmail.ccRecipients) || [],
    (e.gmail && e.gmail.bccRecipients) || [],
  );
  var section = CardService.newCardSection()
    .addWidget(
      CardService.newTextParagraph().setText(
        'Insert a transparent tracking image at the end of this draft. Send confirmation is unavailable; the dashboard will show Prepared.',
      ),
    )
    .addWidget(
      CardService.newTextParagraph().setText(
        'Recipients: ' + (recipients.join(', ') || 'None yet'),
      ),
    )
    .addWidget(CardService.newTextInput().setFieldName('subject').setTitle('Subject for dashboard'))
    .addWidget(
      CardService.newTextButton()
        .setText('Insert tracking image')
        .setOnClickAction(CardService.newAction().setFunctionName('insertPixel')),
    );
  return CardService.newCardBuilder()
    .setHeader(CardService.newCardHeader().setTitle('Prepare tracking'))
    .addSection(section)
    .build();
}
function insertPixel(e) {
  var subject = formValue_(e, 'subject') || '(Gmail draft)';
  var recipients = [].concat(
    (e.gmail && e.gmail.toRecipients) || [],
    (e.gmail && e.gmail.ccRecipients) || [],
    (e.gmail && e.gmail.bccRecipients) || [],
  );
  var result = post_('prepare', {
    identityToken: ScriptApp.getIdentityToken(),
    subject: subject,
    recipients: recipients,
  });
  var image =
    '<img src="' + result.pixelUrl + '" width="1" height="1" alt="" style="display:none" />';
  return CardService.newUpdateDraftActionResponseBuilder()
    .setUpdateDraftBodyAction(
      CardService.newUpdateDraftBodyAction()
        .addUpdateContent(image, CardService.ContentType.MUTABLE_HTML)
        .setUpdateType(CardService.UpdateDraftBodyType.INSERT_AT_END),
    )
    .build();
}
