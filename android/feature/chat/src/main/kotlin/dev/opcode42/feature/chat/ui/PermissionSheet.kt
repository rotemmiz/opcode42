package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.HelpOutline
import androidx.compose.material.icons.filled.Security
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest

/**
 * A8 — Non-dismissible modal sheet for permission.asked events.
 * Three-way reply: Deny / Allow once / Always (the last only when [PermissionRequest.always]
 * is non-empty — older daemons without the `always` field get a two-button row).
 *
 * I3 — Deny-with-feedback: the Deny button reveals a collapsible `OutlinedTextField`
 * ("Send feedback with deny") + a "Send" confirmation. The feedback text is passed as the
 * `message` param of [onReply] (the wire body's optional `message` field, openapi.json:4952).
 * Allow once / Always send `message = null` (approvals carry no feedback).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PermissionSheet(
    permission: PermissionRequest,
    onReply: (reply: String, message: String?) -> Unit,
    isReplying: Boolean,
    pendingCount: Int = 1,
) {
    ModalBottomSheet(
        onDismissRequest = { /* non-dismissible — user must tap a reply */ },
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = SurfaceContainerHigh,
        tonalElevation = 0.dp,
    ) {
        PermissionSheetContent(
            permission = permission,
            onReply = onReply,
            isReplying = isReplying,
            pendingCount = pendingCount,
        )
    }
}

/**
 * The sheet body (icon + title + patterns + 3-way button row + collapsible feedback field),
 * extracted so it can be tested directly without the [ModalBottomSheet] wrapper — Robolectric
 * does not wire click targets through the sheet's Popup, so unit tests exercise
 * [PermissionSheetContent] and the production sheet wraps it unchanged.
 */
@Composable
fun PermissionSheetContent(
    permission: PermissionRequest,
    onReply: (reply: String, message: String?) -> Unit,
    isReplying: Boolean,
    pendingCount: Int = 1,
) {
    val showAlways = permission.always.isNotEmpty()
    var feedbackExpanded by remember { mutableStateOf(false) }
    var feedbackText by remember { mutableStateOf("") }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 24.dp)
            .padding(bottom = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Default.Security,
            contentDescription = null,
            tint = Secondary,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = permission.permission.takeIf { it.isNotBlank() } ?: "Permission required",
            style = MaterialTheme.typography.titleMedium,
            color = OnSurface,
        )
        if (permission.patterns.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            Text(
                text = permission.patterns.joinToString(", "),
                style = MaterialTheme.typography.bodyMedium,
                color = OnSurfaceVariant,
            )
        }
        if (pendingCount > 1) {
            Spacer(Modifier.height(6.dp))
            Text(
                text = "1 of $pendingCount",
                style = MaterialTheme.typography.labelMedium,
                color = OnSurfaceVariant,
            )
        }
        Spacer(Modifier.height(24.dp))
        Row(
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            OutlinedButton(
                onClick = {
                    if (feedbackExpanded) {
                        onReply("reject", feedbackText.takeIf { it.isNotBlank() })
                    } else {
                        feedbackExpanded = true
                    }
                },
                modifier = Modifier.weight(1f),
                enabled = !isReplying,
                colors = ButtonDefaults.outlinedButtonColors(contentColor = Error),
            ) {
                if (isReplying) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = Error,
                    )
                } else {
                    Text("Deny")
                }
            }
            Button(
                onClick = { onReply("once", null) },
                modifier = Modifier.weight(1f),
                enabled = !isReplying,
            ) {
                if (isReplying) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = OnPrimary,
                    )
                } else {
                    Text("Allow once")
                }
            }
            if (showAlways) {
                Button(
                    onClick = { onReply("always", null) },
                    modifier = Modifier.weight(1f),
                    enabled = !isReplying,
                ) { Text("Always") }
            }
        }
        if (feedbackExpanded) {
            Spacer(Modifier.height(16.dp))
            OutlinedTextField(
                value = feedbackText,
                onValueChange = { feedbackText = it },
                label = { Text("Send feedback with deny") },
                modifier = Modifier.fillMaxWidth(),
                minLines = 1,
                maxLines = 4,
                enabled = !isReplying,
            )
            Spacer(Modifier.height(12.dp))
            Button(
                onClick = { onReply("reject", feedbackText.takeIf { it.isNotBlank() }) },
                modifier = Modifier.fillMaxWidth(),
                enabled = !isReplying,
                colors = ButtonDefaults.buttonColors(contentColor = Error),
            ) {
                if (isReplying) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = OnPrimary,
                    )
                } else {
                    Text("Send")
                }
            }
        }
    }
}

/**
 * D3 — Non-modal in-stream card for question.asked events, implementing the full wire contract.
 *
 * Renders directly as a `LazyColumn` item (no `ModalBottomSheet`, no scrim, no swipe). The only
 * exits are Submit (→ [onReply]) and Skip (→ [onReject]). When [resolvedAnswers] is non-null or
 * [resolvedSkipped] is true the card flips to a static, non-tappable history row.
 *
 * A `QuestionRequest` carries one or more `QuestionInfo`s (a multi-step wizard). Each
 * question has a header, full question text, a list of options (single- or multi-select),
 * and an optional "type your own answer" custom row. The reply is sent as
 * `{ answers: string[][] }` — one array of selected labels per question.
 *
 * Single-question requests render as a plain card (no Back/Next). Multi-question requests
 * render as a stepped wizard with Back/Next/Submit and progress segments.
 */
@Composable
fun QuestionCard(
    question: QuestionRequest,
    resolvedAnswers: List<List<String>>?,
    resolvedSkipped: Boolean,
    onReply: (List<List<String>>) -> Unit,
    onReject: () -> Unit,
    isReplying: Boolean,
    modifier: Modifier = Modifier,
    pendingCount: Int = 1,
) {
    val questions = question.questions
    val total = questions.size.coerceAtLeast(1)

    if (resolvedAnswers != null || resolvedSkipped) {
        ResolvedQuestionRow(
            resolvedAnswers = resolvedAnswers,
            resolvedSkipped = resolvedSkipped,
            modifier = modifier,
        )
        return
    }

    // Pending questions stay expanded — the user must answer before continuing — but the
    // card uses the same collapsible shell as every other tool card, so a resolved card
    // (rendered briefly before the store clears the entry) collapses to the header.
    var expanded by remember(question.id) { mutableStateOf(true) }
    var step by remember(question.id) { mutableIntStateOf(0) }
    // Per-question selected labels + custom text. Keyed by question index so a half-answered
    // wizard restores when the card is dismissed and re-shown for the same request id.
    val selections = remember(question.id) {
        mutableStateListOf<MutableSet<String>>().apply {
            repeat(total) { add(mutableSetOf()) }
        }
    }
    val customTexts = remember(question.id) {
        mutableStateListOf<String>().apply {
            repeat(total) { add("") }
        }
    }
    val customActive = remember(question.id) {
        mutableStateListOf<Boolean>().apply {
            repeat(total) { add(false) }
        }
    }

    val headerText = if (questions.isNotEmpty()) {
        questions[step].header.takeIf { it.isNotBlank() } ?: "Question"
    } else {
        "Question"
    }

    CollapsibleToolCard(
        expanded = expanded,
        onToggle = { expanded = !expanded },
        modifier = modifier,
        leading = {
            Icon(
                Icons.Default.HelpOutline,
                contentDescription = null,
                tint = HeaderPurple,
                modifier = Modifier.size(16.dp),
            )
        },
        title = {
            Text(
                text = buildAnnotatedString {
                    withStyle(SpanStyle(color = HeaderPurple, fontWeight = FontWeight.Medium)) {
                        append(headerText)
                    }
                },
                fontFamily = Opcode42Mono,
                fontSize = 13.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        },
        trailing = {
            if (pendingCount > 1) {
                Text(
                    text = "1 of $pendingCount",
                    fontFamily = Opcode42Mono,
                    fontSize = 11.sp,
                    color = OnSurfaceFaint,
                )
            }
        },
    ) {
        if (questions.isEmpty()) {
            // Edge case: a question request with no structured questions. Fall back to a
            // free-text input so the user can still answer.
            Column(Modifier.padding(12.dp)) {
                Text(
                    text = "The agent is waiting for input",
                    style = Opcode42Typography.bodySmall,
                    color = OnSurfaceVariant,
                )
                Spacer(Modifier.height(12.dp))
                OutlinedTextField(
                    value = customTexts[0],
                    onValueChange = { customTexts[0] = it },
                    label = { Text("Your answer") },
                    modifier = Modifier.fillMaxWidth(),
                    minLines = 1,
                    maxLines = 4,
                )
                Spacer(Modifier.height(12.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
                    OutlinedButton(
                        onClick = onReject,
                        modifier = Modifier.weight(1f),
                        enabled = !isReplying,
                    ) { Text("Skip") }
                    Button(
                        onClick = { onReply(listOf(listOf(customTexts[0]))) },
                        modifier = Modifier.weight(1f),
                        enabled = !isReplying && customTexts[0].isNotBlank(),
                    ) { Text("Submit") }
                }
            }
            return@CollapsibleToolCard
        }

        val info = questions[step]
        val isLast = step == total - 1
        val multiple = info.multiple == true
        val allowCustom = info.custom != false

        Column(Modifier.padding(12.dp)) {
            // Progress segments for multi-question wizards.
            if (total > 1) {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                    modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                ) {
                    repeat(total) { i ->
                        Box(
                            Modifier
                                .weight(1f)
                                .height(3.dp)
                                .clip(MaterialTheme.shapes.small)
                                .background(
                                    if (i <= step) MaterialTheme.colorScheme.primary
                                    else MaterialTheme.colorScheme.outlineVariant
                                )
                        )
                    }
                }
            }

            if (info.question.isNotBlank()) {
                Text(
                    text = info.question,
                    style = MaterialTheme.typography.bodyMedium,
                    color = OnSurface,
                )
                Spacer(Modifier.height(12.dp))
            }

            val selected = selections[step]
            fun toggle(label: String) {
                val next = selected.toMutableSet()
                if (multiple) {
                    if (label in next) next.remove(label) else next.add(label)
                } else {
                    next.clear()
                    next.add(label)
                }
                selections[step] = next
                if (customActive[step]) {
                    customActive[step] = false
                    customTexts[step] = ""
                }
            }

            // Options
            if (info.options.isNotEmpty()) {
                info.options.forEach { opt ->
                    val checked = opt.label in selected
                    SelectableOptionRow(
                        label = opt.label,
                        description = opt.description,
                        checked = checked,
                        multiple = multiple,
                        onClick = { toggle(opt.label) },
                    )
                }
            }

            // Custom answer row
            if (allowCustom) {
                val customSelected = customActive[step]
                Spacer(Modifier.height(4.dp))
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.small)
                        .clickable {
                            if (multiple) {
                                customActive[step] = !customSelected
                            } else {
                                selections[step] = mutableSetOf()
                                customActive[step] = !customSelected
                                if (!customSelected) customTexts[step] = ""
                            }
                        }
                        .padding(vertical = 8.dp),
                ) {
                    if (multiple) {
                        Checkbox(
                            checked = customSelected,
                            onCheckedChange = {
                                customActive[step] = it
                                if (!it) customTexts[step] = ""
                            },
                        )
                    } else {
                        RadioButton(
                            selected = customSelected,
                            onClick = {
                                selections[step] = mutableSetOf()
                                customActive[step] = true
                            },
                        )
                    }
                    Text(
                        text = "Type your own answer",
                        style = MaterialTheme.typography.bodyMedium,
                        color = OnSurface,
                    )
                }
                if (customSelected) {
                    Spacer(Modifier.height(8.dp))
                    OutlinedTextField(
                        value = customTexts[step],
                        onValueChange = { customTexts[step] = it },
                        label = { Text("Your answer") },
                        modifier = Modifier.fillMaxWidth(),
                        minLines = 1,
                        maxLines = 4,
                    )
                }
            }

            // Edge case: no options and custom disabled → free-text input anyway (can't
            // send an empty answer).
            if (info.options.isEmpty() && !allowCustom) {
                OutlinedTextField(
                    value = customTexts[step],
                    onValueChange = { customTexts[step] = it },
                    label = { Text("Your answer") },
                    modifier = Modifier.fillMaxWidth(),
                    minLines = 1,
                    maxLines = 4,
                )
            }

            Spacer(Modifier.height(16.dp))

            // Build the answer for the current step → validity check.
            fun currentAnswer(): List<String> = buildList {
                addAll(selected)
                if (customActive[step] && customTexts[step].isNotBlank()) add(customTexts[step])
            }
            val canAdvance = currentAnswer().isNotEmpty() || (info.options.isEmpty() && !allowCustom && customTexts[step].isNotBlank())

            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                if (total > 1 && step > 0) {
                    OutlinedButton(
                        onClick = { step-- },
                        modifier = Modifier.weight(1f),
                        enabled = !isReplying,
                    ) { Text("Back") }
                }
                OutlinedButton(
                    onClick = onReject,
                    modifier = Modifier.weight(1f),
                    enabled = !isReplying,
                ) { Text("Skip") }
                if (isLast) {
                    Button(
                        onClick = {
                            // Collect answers for all questions in order.
                            val answers = (0 until total).map { i ->
                                buildList {
                                    addAll(selections[i])
                                    if (customActive[i] && customTexts[i].isNotBlank()) add(customTexts[i])
                                }
                            }
                            onReply(answers)
                        },
                        modifier = Modifier.weight(1f),
                        enabled = !isReplying && canAdvance,
                    ) { Text("Submit") }
                } else {
                    Button(
                        onClick = { step++ },
                        modifier = Modifier.weight(1f),
                        enabled = !isReplying && canAdvance,
                    ) { Text("Next") }
                }
            }
        }
    }
}

/**
 * The resolved (post-answer) history row. Non-interactive — shows what was answered or that
 * the question was skipped. Lives in the stream until the store clears the pending entry.
 * Uses the same hairline `SurfaceContainer` shell as the tool cards so a resolved question
 * reads as part of the stream, not a leftover modal artifact.
 */
@Composable
private fun ResolvedQuestionRow(
    resolvedAnswers: List<List<String>>?,
    resolvedSkipped: Boolean,
    modifier: Modifier = Modifier,
) {
    val label = when {
        resolvedSkipped -> "Skipped"
        resolvedAnswers != null -> "Answered: " + resolvedAnswers.joinToString(", ") { it.joinToString(", ") }
        else -> "Answered"
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = modifier
            .padding(horizontal = 14.dp, vertical = 4.dp)
            .clip(Opcode42Shapes.sm)
            .background(SurfaceContainer)
            .border(1.dp, Hairline, Opcode42Shapes.sm)
            .heightIn(min = 46.dp)
            .padding(horizontal = 12.dp),
    ) {
        Icon(
            Icons.Default.HelpOutline,
            contentDescription = null,
            tint = OnSurfaceFaint,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = label,
            fontFamily = Opcode42Mono,
            fontSize = 13.sp,
            color = OnSurfaceVariant,
        )
    }
}

@Composable
private fun SelectableOptionRow(
    label: String,
    description: String,
    checked: Boolean,
    multiple: Boolean,
    onClick: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.small)
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
    ) {
        if (multiple) {
            Checkbox(checked = checked, onCheckedChange = { onClick() })
        } else {
            RadioButton(selected = checked, onClick = onClick)
        }
        Column(Modifier.weight(1f)) {
            Text(
                text = label,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSecondaryContainer,
            )
            if (description.isNotBlank()) {
                Text(
                    text = description,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSecondaryContainer,
                )
            }
        }
    }
}