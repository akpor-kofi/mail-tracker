function post_(path, body) {
  var response = UrlFetchApp.fetch(PUBLIC_URL + '/api/v1/addon/' + path, {
    method: 'post',
    contentType: 'application/json',
    payload: JSON.stringify(body),
    muteHttpExceptions: true,
  });
  if (response.getResponseCode() !== 200) {
    var status = response.getResponseCode();
    if (status === 401 && path === 'pair') {
      throw new Error(
        'This pairing code may have already been used, expired, or be invalid. If you already paired successfully, open a Gmail draft and choose Prepare tracking. Otherwise, generate a new code in Dashboard → Settings and use the matching Gmail account.',
      );
    }
    if (status === 401 && path === 'prepare') {
      throw new Error(
        'Could not verify this Gmail account. Pair it using a new code from Dashboard → Settings. If pairing also fails, check the add-on client ID in your instance settings.',
      );
    }
    if (status === 429) throw new Error('Too many attempts. Wait a minute and try again.');
    throw new Error('Mail Tracker is temporarily unavailable. Try again shortly.');
  }
  return JSON.parse(response.getContentText());
}
function formValue_(e, name) {
  var inputs = e.commonEventObject && e.commonEventObject.formInputs;
  var item = inputs && inputs[name];
  return (item && item.stringInputs && item.stringInputs.value && item.stringInputs.value[0]) || '';
}
function homeCard() {
  return pairingCard_('', '');
}
function pairingCard_(message, code) {
  var section = CardService.newCardSection();
  if (message) section.addWidget(CardService.newTextParagraph().setText(message));
  section
    .addWidget(
      CardService.newTextParagraph().setText(
        'Pair this Gmail account with your self-hosted Mail Tracker instance. Generate a one-use code in Dashboard → Settings.',
      ),
    )
    .addWidget(
      CardService.newTextInput().setFieldName('pair_code').setTitle('Pairing code').setValue(code),
    )
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
  var code = formValue_(e, 'pair_code').trim();
  if (!code) return pairingError_('Enter a new pairing code from Dashboard → Settings.', code);
  try {
    post_('pair', { code: code, identityToken: ScriptApp.getIdentityToken() });
  } catch (error) {
    return pairingError_(
      error.message || 'Could not connect to Mail Tracker. Try again shortly.',
      code,
    );
  }
  var card = CardService.newCardBuilder()
    .setHeader(CardService.newCardHeader().setTitle('Gmail account paired'))
    .addSection(
      CardService.newCardSection().addWidget(
        CardService.newTextParagraph().setText(
          'Pairing is complete. You do not need to enter this code again. To track an email, open a Gmail draft and choose Prepare tracking in the compose toolbar.',
        ),
      ),
    )
    .build();
  return CardService.newActionResponseBuilder()
    .setNavigation(CardService.newNavigation().updateCard(card))
    .setNotification(CardService.newNotification().setText('This Gmail account is paired.'))
    .build();
}
function pairingError_(message, code) {
  return CardService.newActionResponseBuilder()
    .setNavigation(CardService.newNavigation().updateCard(pairingCard_(message, code)))
    .build();
}
function notice_(message) {
  return CardService.newActionResponseBuilder()
    .setNotification(CardService.newNotification().setText(message))
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
      CardService.newTextInput()
        .setFieldName('link_url')
        .setTitle('Optional link to insert (https://…)'),
    )
    .addWidget(CardService.newTextInput().setFieldName('link_text').setTitle('Link label'))
    .addWidget(
      CardService.newTextParagraph().setText(
        'Only the link entered here is tracked. Existing draft links are unchanged.',
      ),
    )
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
  var result;
  try {
    result = post_('prepare', {
      identityToken: ScriptApp.getIdentityToken(),
      subject: subject,
      recipients: recipients,
    });
  } catch (error) {
    return notice_(error.message || 'Could not connect to Mail Tracker. Try again shortly.');
  }
  var link = '';
  var destination = formValue_(e, 'link_url').trim();
  if (destination) {
    try {
      var tracked = post_('link', {
        identityToken: ScriptApp.getIdentityToken(),
        conversationId: result.conversationId,
        destination: destination,
      });
      var label = formValue_(e, 'link_text') || 'View link';
      link =
        '<p><a href="' +
        tracked.url +
        '">' +
        label
          .replace(/&/g, '&amp;')
          .replace(/</g, '&lt;')
          .replace(/>/g, '&gt;')
          .replace(/"/g, '&quot;') +
        '</a></p>';
    } catch (error) {
      return notice_(error.message || 'Could not create tracked link.');
    }
  }
  var image =
    '<img src="' + result.pixelUrl + '" width="1" height="1" alt="" style="display:none" />';
  return CardService.newUpdateDraftActionResponseBuilder()
    .setUpdateDraftBodyAction(
      CardService.newUpdateDraftBodyAction()
        .addUpdateContent(link + image, CardService.ContentType.MUTABLE_HTML)
        .setUpdateType(CardService.UpdateDraftBodyType.INSERT_AT_END),
    )
    .build();
}
