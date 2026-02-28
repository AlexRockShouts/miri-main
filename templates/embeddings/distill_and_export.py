# distill_qwen3_embedding_0.6b_fast_msgpack.py
"""
Distills Qwen/Qwen3-Embedding-0.6B into static token embeddings (Model2Vec-style).
Exports as msgpack for fastest loading in Go.

Dependencies:
    pip install sentence-transformers torch numpy scikit-learn msgpack
"""

import os
import torch
import numpy as np
from sentence_transformers import SentenceTransformer
from sklearn.decomposition import PCA
import msgpack

# ================= CONFIG =================
MODEL_NAME = "Qwen/Qwen3-Embedding-0.6B"
PCA_DIMS   = 384               # 256 / 384 / 512 / None (= keep up to 1024)
OUTPUT_FILE = f"static_qwen3_embedding_0.6b_pca{PCA_DIMS if PCA_DIMS else 'full'}.msgpack"
# ==========================================

def main():
    torch.set_default_device("cpu")
    print("Device: cpu (Apple Silicon safe)")

    print(f"\nLoading {MODEL_NAME} ... (~1–1.5 GB download first time)")
    model = SentenceTransformer(
        MODEL_NAME,
        device="cpu",
        trust_remote_code=True
    )

    # Extract embedding layer (Qwen models usually use get_input_embeddings)
    try:
        embedding_layer = model[0].auto_model.get_input_embeddings()
    except AttributeError:
        try:
            embedding_layer = model[0].auto_model.embeddings.word_embeddings
        except AttributeError:
            raise RuntimeError("Embedding layer not found. Inspect: print(dir(model[0].auto_model))")

    print(f"Embedding layer: {embedding_layer.__class__.__name__}")
    print(f"Original max dim: {embedding_layer.embedding_dim}")
    print(f"Vocab size: {embedding_layer.num_embeddings:,}")

    tokenizer = model.tokenizer
    vocab = tokenizer.get_vocab()

    print("\nExtracting token embeddings...")
    embed_matrix = np.zeros(
        (len(vocab), embedding_layer.embedding_dim),
        dtype=np.float32
    )

    token_list = [None] * len(vocab)
    for token, token_id in vocab.items():
        vec = embedding_layer.weight[token_id].detach().cpu().numpy()
        embed_matrix[token_id] = vec
        token_list[token_id] = token

    # PCA (optional – truncate early for Matryoshka-like efficiency)
    final_dim = PCA_DIMS if PCA_DIMS is not None else embedding_layer.embedding_dim
    if PCA_DIMS is not None and PCA_DIMS < embedding_layer.embedding_dim:
        print(f"→ PCA to {PCA_DIMS} dims ...")
        pca = PCA(n_components=PCA_DIMS)
        reduced_matrix = pca.fit_transform(embed_matrix)
        print(f"Explained variance: {pca.explained_variance_ratio_.sum():.4f}")
    else:
        reduced_matrix = embed_matrix
        print("No PCA (full dimension)")

    # Build dict
    print("Building msgpack data...")
    token_embeddings = {}
    for i, token in enumerate(token_list):
        if token is not None:
            token_embeddings[token] = reduced_matrix[i].tolist()

    export_data = {
        "dim": final_dim,
        "embeddings": token_embeddings,
        "original_model": MODEL_NAME,
        "pca_components": PCA_DIMS if PCA_DIMS else None,
        "note": "Static Model2Vec-style – average tokens in Go. Supports flexible dims."
    }

    with open(OUTPUT_FILE, "wb") as f:
        msgpack.dump(export_data, f, use_bin_type=True)

    size_mb = os.path.getsize(OUTPUT_FILE) / (1024 * 1024)
    print(f"\nDone!")
    print(f"Exported {len(token_embeddings):,} tokens @ {final_dim} dims")
    print(f"File: {OUTPUT_FILE}")
    print(f"Size: {size_mb:.1f} MB")

if __name__ == "__main__":
    main()