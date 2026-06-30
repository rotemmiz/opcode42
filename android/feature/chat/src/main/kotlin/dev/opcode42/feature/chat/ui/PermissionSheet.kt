package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Security
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
 * A8 — Modal sheet for question.asked events.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuestionSheet(
    question: QuestionRequest,
    onReply: (String) -> Unit,
    onReject: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    var answer = androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf("") }

    ModalBottomSheet(
        onDismissRequest = { /* non-dismissible */ },
        sheetState = sheetState,
        containerColor = SurfaceContainerHigh,
        tonalElevation = 0.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(bottom = 32.dp),
        ) {
            Text(
                text = question.message ?: "The agent has a question:",
                style = MaterialTheme.typography.titleMedium,
                color = OnSurface,
            )
            Spacer(Modifier.height(16.dp))
            OutlinedTextField(
                value = answer.value,
                onValueChange = { answer.value = it },
                label = { Text("Your answer") },
                modifier = Modifier.fillMaxWidth(),
                minLines = 2,
            )
            Spacer(Modifier.height(16.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                OutlinedButton(
                    onClick = onReject,
                    modifier = Modifier.weight(1f),
                ) { Text("Skip") }
                Button(
                    onClick = { onReply(answer.value) },
                    modifier = Modifier.weight(1f),
                    enabled = answer.value.isNotBlank(),
                ) { Text("Reply") }
            }
        }
    }
}
