package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
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
import androidx.compose.ui.unit.dp
import dev.opcode42.core.model.PermissionRequest
import dev.opcode42.core.model.QuestionRequest

/**
 * A8 — Non-dismissible modal sheet for permission.asked events.
 * Mirrors the PermissionPrompt from plan 07.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PermissionSheet(
    permission: PermissionRequest,
    onApprove: () -> Unit,
    onDeny: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = { /* non-dismissible — user must tap Approve/Deny */ },
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = SurfaceContainerHigh,
        tonalElevation = 0.dp,
    ) {
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
                text = permission.title ?: "Permission Required",
                style = MaterialTheme.typography.titleMedium,
                color = OnSurface,
            )
            permission.description?.let { desc ->
                Spacer(Modifier.height(8.dp))
                Text(
                    text = desc,
                    style = MaterialTheme.typography.bodyMedium,
                    color = OnSurfaceVariant,
                )
            }
            Spacer(Modifier.height(24.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                OutlinedButton(
                    onClick = onDeny,
                    modifier = Modifier.weight(1f),
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = Error),
                ) { Text("Deny") }
                Button(
                    onClick = onApprove,
                    modifier = Modifier.weight(1f),
                ) { Text("Approve") }
            }
        }
    }
}

/**
 * D3 — Modal sheet for question.asked events, implementing the full wire contract.
 *
 * A `QuestionRequest` carries one or more `QuestionInfo`s (a multi-step wizard). Each
 * question has a header, full question text, a list of options (single- or multi-select),
 * and an optional "type your own answer" custom row. The reply is sent as
 * `{ answers: string[][] }` — one array of selected labels per question.
 *
 * Single-question requests render as a plain sheet (no tabs). Multi-question requests
 * render as a stepped wizard with Back/Next/Submit and progress segments.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuestionSheet(
    question: QuestionRequest,
    onReply: (List<List<String>>) -> Unit,
    onReject: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val questions = question.questions
    val total = questions.size.coerceAtLeast(1)
    var step by remember(question.id) { mutableIntStateOf(0) }
    // Per-question selected labels + custom text. Keyed by question index so a half-answered
    // wizard restores when the sheet is dismissed and re-shown for the same request id.
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

    ModalBottomSheet(
        onDismissRequest = onReject,
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
        tonalElevation = 0.dp,
    ) {
        if (questions.isEmpty()) {
            // Edge case: a question request with no structured questions. Fall back to a
            // free-text input so the user can still answer (matches the plan's
            // "custom=false and no options → text input" fallback, generalized).
            FreeTextQuestionFallback(
                title = "The agent is waiting for input",
                onReply = { text -> onReply(listOf(listOf(text))) },
                onReject = onReject,
            )
            return@ModalBottomSheet
        }

        val info = questions[step]
        val isLast = step == total - 1
        val multiple = info.multiple == true
        val allowCustom = info.custom != false

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(bottom = 32.dp),
        ) {
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

            Text(
                text = info.header.takeIf { it.isNotBlank() } ?: "Question",
                style = MaterialTheme.typography.titleMedium,
                color = OnSurface,
            )
            if (info.question.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    text = info.question,
                    style = MaterialTheme.typography.bodyMedium,
                    color = OnSurfaceVariant,
                )
            }
            Spacer(Modifier.height(16.dp))

            val selected = selections[step]
            fun toggle(label: String) {
                if (multiple) {
                    if (label in selected) selected.remove(label) else selected.add(label)
                } else {
                    selected.clear()
                    selected.add(label)
                }
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
                                selected.clear()
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
                                selected.clear()
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
                    ) { Text("Back") }
                }
                OutlinedButton(
                    onClick = onReject,
                    modifier = Modifier.weight(1f),
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
                        enabled = canAdvance,
                    ) { Text("Submit") }
                } else {
                    Button(
                        onClick = { step++ },
                        modifier = Modifier.weight(1f),
                        enabled = canAdvance,
                    ) { Text("Next") }
                }
            }
        }
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
                color = OnSurface,
            )
            if (description.isNotBlank()) {
                Text(
                    text = description,
                    style = MaterialTheme.typography.bodySmall,
                    color = OnSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun FreeTextQuestionFallback(
    title: String,
    onReply: (String) -> Unit,
    onReject: () -> Unit,
) {
    var answer by remember { androidx.compose.runtime.mutableStateOf("") }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 24.dp)
            .padding(bottom = 32.dp),
    ) {
        Text(text = title, style = MaterialTheme.typography.titleMedium, color = OnSurface)
        Spacer(Modifier.height(16.dp))
        OutlinedTextField(
            value = answer,
            onValueChange = { answer = it },
            label = { Text("Your answer") },
            modifier = Modifier.fillMaxWidth(),
            minLines = 2,
        )
        Spacer(Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
            OutlinedButton(onClick = onReject, modifier = Modifier.weight(1f)) { Text("Skip") }
            Button(
                onClick = { onReply(answer) },
                modifier = Modifier.weight(1f),
                enabled = answer.isNotBlank(),
            ) { Text("Reply") }
        }
    }
}
